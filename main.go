package main

import (
	"log"
	"os"
	"os/signal"
	"time"

	ssdp "github.com/koron/go-ssdp"
)

func main() {
	ssdp.Logger = log.New(os.Stderr, "[SSDP] ", log.LstdFlags)

	ad, err := ssdp.Advertise(
		"my:device",                        // send as "ST"
		"unique:id",                        // send as "USN"
		"http://192.168.0.1:57086/foo.xml", // send as "LOCATION"
		"go-ssdp sample",                   // send as "SERVER"
		1800)                               // send as "maxAge" in "CACHE-CONTROL"
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
