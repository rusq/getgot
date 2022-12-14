package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/mitchellh/go-ps"
)

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	log.Println("get got started")
	return supress(ctx, 5*time.Second, "du", "jamf")
}

func supress(ctx context.Context, interval time.Duration, processes ...string) error {
	var pm = make(map[string]bool, len(processes))
	for _, v := range processes {
		pm[v] = true
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := wipe(pm); err != nil {
				log.Println("wipe error:", err)
			}
		}

	}
}

// wipe kills all processes in the pm map
func wipe(pm map[string]bool) error {
	pp, err := ps.Processes()
	if err != nil {
		return err
	}

	for _, p := range pp {
		if _, ok := pm[p.Executable()]; !ok {
			continue
		}
		proc, err := os.FindProcess(p.Pid())
		if err != nil {
			return err
		}
		if err := proc.Kill(); err != nil {
			return fmt.Errorf("failed to kill %s: %w", p.Executable(), err)
		}
		log.Printf("killed %s (%d) parent: %d", p.Executable(), p.Pid(), p.PPid())
	}
	return nil
}
