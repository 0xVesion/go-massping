package main

import (
	"flag"
	"fmt"

	"github.com/0xVesion/go-massping"
)

func main() {
	var subnet string

	flag.StringVar(&subnet, "subnet", "192.168.0", "the prefix of the subnet to scan")
	flag.Parse()

	p, err := massping.New()
	if err != nil {
		panic(err)
	}

	fmt.Printf("Searching for hosts in %s.0...\n", subnet)
	for i := 1; i < 255; i++ {
		ip := fmt.Sprintf("%s.%d", subnet, i)
		p.Ping(ip)
	}

	ips := p.Wait()
	fmt.Printf("Found %d hosts:\n", len(ips))
	for _, ip := range ips {
		fmt.Printf("\t- %s\n", ip)
	}
}
