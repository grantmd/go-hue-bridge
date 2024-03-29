package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"time"

	"github.com/hashicorp/mdns"
	ssdp "github.com/koron/go-ssdp"
)

const (
	httpPort  = 80
	httpsPort = 443
)

var (
	macAddress string
	ipAddress  string
	bridgeID   string
)

func main() {
	var err error
	macAddress, err = getMacAddr()
	if err != nil {
		panic(err)
	}
	if macAddress == "" {
		panic("Could not find mac address")
	}
	log.Printf("MAC Address: %s", macAddress)

	mac := strings.ReplaceAll(macAddress, ":", "")
	bridgeID = mac[len(mac)-6:]
	log.Printf("Bridge ID: %s", bridgeID)

	//////////////////////////////////////////////////////

	router := http.NewServeMux()
	router.Handle("/api/", serveAPI())
	router.Handle("/description.xml", serveDescriptionXML())
	router.Handle("/", serveIndex())

	s := &http.Server{
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		ErrorLog:     log.New(os.Stderr, "[HTTP] ", log.LstdFlags),
	}
	go s.ListenAndServe()
	go s.ListenAndServeTLS("cert.pem", "key.pem")

	//////////////////////////////////////////////////////

	// Setup our service export
	host, _ := os.Hostname()
	info := []string{fmt.Sprintf("Philips Hue - %s", bridgeID)}
	service, _ := mdns.NewMDNSService(host, "hue", fmt.Sprintf("%s._hue._tcp._local", bridgeID), "", 8000, nil, info)

	// Create the mDNS server, defer shutdown
	server, _ := mdns.NewServer(&mdns.Config{Zone: service})
	defer server.Shutdown()

	//////////////////////////////////////////////////////

	ssdp.Logger = log.New(os.Stderr, "[SSDP] ", log.LstdFlags)

	ipAddress, err = getLocalIP()
	if err != nil {
		panic(err)
	}
	if ipAddress == "" {
		panic("Could not find ip address")
	}

	ad, err := ssdp.Advertise(
		"urn:schemas-upnp-org:device:Basic:1",               // send as "ST"
		fmt.Sprintf("38323636-4558-4dda-9188-%s", mac),      // send as "USN"
		fmt.Sprintf("http://%s/description.xml", ipAddress), // send as "LOCATION"
		"FreeRTOS/6.0.5, UPnP/1.0, IpBridge/0.1",            // send as "SERVER"
		1200)                                                // send as "maxAge" in "CACHE-CONTROL"
	if err != nil {
		panic(err)
	}

	// run Advertiser infinitely until CTRL-C is pressed.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)

	aliveTick := time.Tick(300 * time.Second)

loop:
	for {
		select {
		case <-aliveTick:
			ad.Alive()
		case <-quit:
			break loop
		}
	}

	// send/multicast "byebye" message.
	ad.Bye()
	// teminate Advertiser.
	ad.Close()
}

func getMacAddr() (addr string, err error) {
	ifas, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	for _, ifa := range ifas {
		// Interface is up and has a mac address
		if ifa.Flags&net.FlagUp != 0 && bytes.Compare(ifa.HardwareAddr, nil) != 0 {
			// Skip locally administered addresses
			if ifa.HardwareAddr[0]&2 == 2 {
				continue
			}

			// Skip vEthernet addresses. I don't love this, but I need it
			if strings.Contains(ifa.Name, "vEthernet") {
				continue
			}

			// Interface still has an address, I guess
			addr = ifa.HardwareAddr.String()
			if addr != "" {
				return addr, nil
			}
		}
	}

	return "", nil
}

// getLocalIP returns the non loopback local IP of the host
func getLocalIP() (addr string, err error) {
	// I don't love this method, but iterating over all the interfaces doesn't work for the same reason as getMacAddr above
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "", err
	}

	defer conn.Close()
	addr = conn.LocalAddr().String()
	return addr[:strings.IndexByte(addr, ':')], nil // Remove port from this address
}

func serveIndex() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		fmt.Println("Request:", r.Method, r.URL)

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "Hello, World!")
	})
}

func serveDescriptionXML() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		fmt.Println("Request:", r.Method, r.URL)

		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)

		xml := `<?xml version="1.0" ?>
<root xmlns="urn:schemas-upnp-org:device-1-0">
<specVersion><major>1</major><minor>0</minor></specVersion>
<URLBase>http://{ip}:80/</URLBase>
<device>
	<deviceType>urn:schemas-upnp-org:device:Basic:1</deviceType>
	<friendlyName>Philips hue ({ip})</friendlyName>
	<manufacturer>Royal Philips Electronics</manufacturer>
	<manufacturerURL>http://www.philips.com</manufacturerURL>
	<modelDescription>Philips hue Personal Wireless Lighting</modelDescription>
	<modelName>Philips hue bridge 2012</modelName>
	<modelNumber>929000226503</modelNumber>
	<modelURL>http://www.meethue.com</modelURL>
	<serialNumber>{mac}</serialNumber>
	<UDN>uuid:2f402f80-da50-11e1-9b23-{mac}</UDN>
	<presentationURL>index.html</presentationURL>
	<iconList>
	<icon>
		<mimetype>image/png</mimetype>
		<height>48</height>
		<width>48</width>
		<depth>24</depth>
		<url>hue_logo_0.png</url>
	</icon>
	<icon>
		<mimetype>image/png</mimetype>
		<height>120</height>
		<width>120</width>
		<depth>24</depth>
		<url>hue_logo_3.png</url>
	</icon>
	</iconList>
</device>
</root>`

		mac := strings.ReplaceAll(macAddress, ":", "")
		mac = strings.ToLower(mac)

		xml = strings.ReplaceAll(xml, "{mac}", mac)
		xml = strings.ReplaceAll(xml, "{ip}", ipAddress)

		fmt.Fprintln(w, xml)
	})
}

func serveAPI() http.Handler {

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		fmt.Println("API Request:", r.Method, r.URL)

		var output = "{}"

		// Example: /api/nouser/config
		var configPath = regexp.MustCompile(`^\/api\/(.+)\/config$`)
		var apiPath = regexp.MustCompile(`^\/api\/(.*)$`)
		path := r.URL.Path
		switch {
		case configPath.MatchString(path):
			output = getConfig()
		case apiPath.MatchString(path):
			switch r.Method {
			case "POST":
				// Always succeed with authentication, even though the "link" button isn't pushed
				// TODO: Make this a generic method
				output = returnSuccess("username", "api")
			case "GET":
				output = getFullConfig()
			}
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, output)
	})
}

type hubConfig struct {
	Name           string                  `json:"name"`
	BridgeID       string                  `json:"bridgeid"`
	PortalServices bool                    `json:"portalservices"`
	IPAddress      string                  `json:"ipaddress"`
	Gateway        string                  `json:"gateway"`
	NetMask        string                  `json:"netmask"`
	ProxyAddress   string                  `json:"proxyaddress"`
	ProxyPort      int                     `json:"proxyport"`
	MacAddress     string                  `json:"mac"`
	SWVersion      string                  `json:"swversion"`
	LinkButton     bool                    `json:"linkbutton"`
	SWupdate       hubSWUpdate             `json:"swupdate"`
	APIVersion     string                  `json:"apiversion"`
	DHCP           bool                    `json:"dhcp"`
	Whitelist      map[string]hubWhitelist `json:"whitelist,omitempty"`
	UTC            string                  `json:"utc"`
}

type hubSWUpdate struct {
	Text        string `json:"text"`
	Notify      bool   `json:"notify"`
	UpdateState int    `json:"updatestate"`
	URL         string `json:"url"`
}

type hubWhitelist struct {
	Name string `json:"name"`
}

func getConfig() string {
	response := &hubConfig{
		Name:           "Go Hue Bridge",
		BridgeID:       bridgeID,
		SWVersion:      "81012917",
		PortalServices: false,
		LinkButton:     false,
		MacAddress:     macAddress,
		DHCP:           true, // TODO
		IPAddress:      ipAddress,
		NetMask:        "255.255.255.0", // TODO
		Gateway:        "192.168.1.1",   // TODO
		APIVersion:     "1.3.0",
		UTC:            time.Now().Format(time.RFC3339),
	}

	response.Whitelist = map[string]hubWhitelist{
		"api": hubWhitelist{
			Name: "clientname#devicename",
		},
	}

	b, err := json.MarshalIndent(response, "", "\t")
	if err != nil {
		log.Fatal(err)
	}

	return string(b)
}

type Light struct {
	Name      string     `json:"name"`
	Type      string     `json:"type"`
	ModelID   string     `json:"modelid"`
	UniqueID  string     `json:"uniqueid"`
	SWVersion string     `json:"swversion"`
	State     lightState `json:"state"`
}

type lightState struct {
	On         bool   `json:"on"`
	Brightness int    `json:"bri"`
	Alert      string `json:"alert"`
	Reachable  bool   `json:"reachable"`
}

type Schedule struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Time        string `json:"time"`
}

type Group struct {
	Name   string   `json:"name"`
	Lights []string `json:"lights"`
}

type Scene struct {
	Name        string   `json:"name"`
	Lights      []string `json:"lights"`
	Owner       string   `json:"owner"`
	Recycle     bool     `json:"recycle"`
	Locked      bool     `json:"locked"`
	Picture     string   `json:"picture"`
	LastUpdated string   `json:"lastupdated"`
	Version     string   `json:"version"`
}

type hubFullConfig struct {
	Lights        []Light       `json:"lights"`
	Groups        []Group       `json:"groups"`
	Config        hubConfig     `json:"config"`
	Schedules     []Schedule    `json:"schedules"`
	Scenes        []Scene       `json:"scenes"`
	Rules         []interface{} `json:"rules"`
	Sensors       []interface{} `json:"sensors"`
	ResourceLinks []interface{} `json:"resourcelinks"`
}

func getFullConfig() string {
	response := &hubFullConfig{}

	response.Lights = []Light{}
	response.Groups = []Group{}
	response.Schedules = []Schedule{}
	response.Scenes = []Scene{}

	// TODO: Dedupe this logic with getConfig()
	response.Config = hubConfig{
		Name:           "Go Hue Bridge",
		BridgeID:       bridgeID,
		SWVersion:      "81012917",
		PortalServices: false,
		LinkButton:     false,
		MacAddress:     macAddress,
		DHCP:           true, // TODO
		IPAddress:      ipAddress,
		NetMask:        "255.255.255.0", // TODO
		Gateway:        "192.168.1.1",   // TODO
		APIVersion:     "1.3.0",
		UTC:            time.Now().Format(time.RFC3339),
	}

	response.Config.Whitelist = map[string]hubWhitelist{
		"api": hubWhitelist{
			Name: "clientname#devicename",
		},
	}

	b, err := json.MarshalIndent(response, "", "\t")
	if err != nil {
		log.Fatal(err)
	}

	return string(b)

}

// TODO: Make this a proper object and marshal to json
func returnSuccess(id, value string) string {
	return `[
	{
		"success": {
			"` + id + `": "` + value + `"
		}
	}
]`
}
