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
	HTTP_PORT = 80
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

	router := http.NewServeMux()
	router.Handle("/", serveIndex())

	s := &http.Server{
		Addr:         fmt.Sprintf(":%d", HTTP_PORT),
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		ErrorLog:     log.New(os.Stderr, "[HTTP] ", log.LstdFlags),
	}
	go s.ListenAndServe()

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
	fmt.Println(mac)

	ip, err := getLocalIP()
	if err != nil {
		panic(err)
	}
	if ip == "" {
		panic("Could not find ip address")
	}

	ad, err := ssdp.Advertise(
		"urn:schemas-upnp-org:device:Basic:1",                      // send as "ST"
		fmt.Sprintf("38323636-4558-4dda-9188-%s", mac),             // send as "USN"
		fmt.Sprintf("http://%s:%d/description.xml", ip, HTTP_PORT), // send as "LOCATION"
		"FreeRTOS/6.0.5, UPnP/1.0, IpBridge/0.1",                   // send as "SERVER"
		1200)                                                       // send as "maxAge" in "CACHE-CONTROL"
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
		if ifa.Flags&net.FlagUp != 0 && bytes.Compare(ifa.HardwareAddr, nil) != 0 {
			// Skip locally administered addresses
			if ifa.HardwareAddr[0]&2 == 2 {
				continue
			}

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
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}
	for _, address := range addrs {
		// check the address type and if it is not a loopback the display it
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String(), nil
			}
		}
	}
	return "", err
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
