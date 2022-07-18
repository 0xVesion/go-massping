package massping

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"net"
	"sync"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

type Queue interface {
	Timeout(time.Duration)
	Ping(ip string)
	Wait() []string
}

type queue struct {
	src       *icmp.PacketConn
	timeout   time.Duration
	pings     *map[string]ping
	pingsLock *sync.Mutex
	results   chan interface{}
	hosts     []string
	wg        *sync.WaitGroup
}

type ping struct {
	id  int
	seq int
	dst *net.IPAddr
}

func New() (Queue, error) {
	conn, err := listen()
	if err != nil {
		return nil, err
	}

	pq := &queue{
		timeout:   time.Millisecond * 50,
		pings:     &map[string]ping{},
		src:       conn,
		pingsLock: &sync.Mutex{},
		results:   make(chan interface{}),
		wg:        &sync.WaitGroup{},
	}

	go pq.handleResults()

	return pq, nil
}

func (pq *queue) Wait() []string {
	pq.wg.Wait()
	close(pq.results)
	pq.src.Close()

	return pq.hosts
}

func (pq *queue) Timeout(t time.Duration) {
	pq.timeout = t
}

func (pq *queue) Ping(ip string) {
	pq.wg.Add(1)
	go func() {
		p := ping{
			id:  rand.Intn(math.MaxInt16),
			seq: rand.Intn(math.MaxInt16),
			dst: parseIP(ip),
		}
		pq.pingsLock.Lock()
		(*pq.pings)[ip] = p
		pq.pingsLock.Unlock()

		err := pq.sendPing(p)
		if err != nil {
			pq.results <- err
			return
		}
		go pq.receivePing()
	}()
}

func (pq *queue) sendPing(p ping) error {
	bytes, err := (&icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{
			ID:   p.id,
			Seq:  p.seq,
			Data: []byte{},
		},
	}).Marshal(nil)
	if err != nil {
		return fmt.Errorf("couldn't marshal echo message for %s: %w", p.dst, err)
	}

	_, err = pq.src.WriteTo(bytes, p.dst)
	if err != nil {
		return fmt.Errorf("error whilst sending echo message to %s: %w", p.dst, err)
	}

	return nil
}

func parseIP(ip string) *net.IPAddr {
	return &net.IPAddr{IP: net.ParseIP(ip)}
}

func listen() (*icmp.PacketConn, error) {
	ipV4Network := "ip4:icmp"
	ipV4ListenAddress := "0.0.0.0"
	conn, err := icmp.ListenPacket(ipV4Network, ipV4ListenAddress)
	if err != nil {
		return nil, fmt.Errorf("couldn't start listening for packets. Are you root?: %w", err)
	}

	return conn, nil
}

func (pq *queue) handleResults() {
	for res := range pq.results {
		if r, ok := res.(string); ok {
			pq.hosts = append(pq.hosts, r)
		}

		pq.wg.Done()
	}
}

func (pq *queue) receivePing() {
	err := setTimeout(pq.src, pq.timeout)
	if err != nil {
		pq.results <- err
		return
	}

	msgBytes, ip, err := readFrom(pq.src)
	if err != nil {
		pq.results <- err
		return
	}

	p := pq.getPing(ip)
	err = validateMessage(msgBytes, p)
	if err != nil {
		pq.results <- fmt.Errorf("%s: %w", ip, err)
		return
	}

	pq.results <- ip.String()
}

func (pq *queue) getPing(ip net.Addr) ping {
	pq.pingsLock.Lock()
	p := (*pq.pings)[ip.String()]
	pq.pingsLock.Unlock()

	return p
}

func setTimeout(conn *icmp.PacketConn, timeout time.Duration) error {
	err := conn.SetReadDeadline(time.Now().Add(timeout))
	if err != nil {
		return fmt.Errorf("couldn't set response deadline: %w", err)
	}

	return nil
}

func validateMessage(msgBytes []byte, p ping) error {
	icmpV4Protocol := 1
	msg, err := icmp.ParseMessage(icmpV4Protocol, msgBytes)
	if err != nil {
		return fmt.Errorf("couldn't parse response: %w", err)
	}

	if msg.Type != ipv4.ICMPTypeEchoReply {
		return fmt.Errorf("invalid response: %v", msg)
	}

	msgBody := msg.Body.(*icmp.Echo)
	if msgBody.ID != p.id || msgBody.Seq != p.seq {
		return fmt.Errorf("invalid response: %v", msg)
	}

	return nil
}

func readFrom(src *icmp.PacketConn) ([]byte, net.Addr, error) {
	bytes := make([]byte, 512)
	_, ip, err := src.ReadFrom(bytes)
	if err != nil {
		ne := &net.OpError{}
		if ok := errors.As(err, &ne); ok {
			if ne.Timeout() {
				return nil, nil, fmt.Errorf("couldn't set response deadline: %w", err)
			}
		}

		return nil, nil, fmt.Errorf("couldn't receive response: %w", err)
	}

	return bytes, ip, nil
}
