package main

import (
	"bytes"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/hashicorp/mdns"
	ssdp "github.com/koron/go-ssdp"
)

const (
	httpPort  = 80
	httpsPort = 443
)

func main() {
	mac, err := getMacAddr()
	if err != nil {
		panic(err)
	}
	if mac == "" {
		panic("Could not find mac address")
	}

	mac = strings.ReplaceAll(mac, ":", "")
	bridgeID := mac[len(mac)-6:]

	//////////////////////////////////////////////////////

	router := http.NewServeMux()
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

	ip, err := getLocalIP()
	if err != nil {
		panic(err)
	}
	if ip == "" {
		panic("Could not find ip address")
	}

	ad, err := ssdp.Advertise(
		"urn:schemas-upnp-org:device:Basic:1",          // send as "ST"
		fmt.Sprintf("38323636-4558-4dda-9188-%s", mac), // send as "USN"
		fmt.Sprintf("http://%s/description.xml", ip),   // send as "LOCATION"
		"FreeRTOS/6.0.5, UPnP/1.0, IpBridge/0.1",       // send as "SERVER"
		1200)                                           // send as "maxAge" in "CACHE-CONTROL"
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

		fmt.Println("Request:", r.URL)

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "Hello, World!")
	})
}
