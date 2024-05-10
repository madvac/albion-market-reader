package main

import (
	"os"
	"strings"
	"time"

	"github.com/ao-data/albiondata-client/client"
	"github.com/ao-data/albiondata-client/log"
	"github.com/ao-data/albiondata-client/systray"

	"github.com/ao-data/go-githubupdate/updater"
)

var version string

func init() {
	client.ConfigGlobal.SetupFlags()
}

func main() {
	if client.ConfigGlobal.PrintVersion {
		log.Infof("Albion Data Client, version: %s", version)
		return
	}

	startUpdater()

	go systray.Run()

	c := client.NewClient(version)
	err := c.Run()
	if err != nil {
		log.Error(err)
		log.Error("The program encountered an error. Press any key to close this window.")
		var b = make([]byte, 1)
		_, _ = os.Stdin.Read(b)
	}

}

func startUpdater() {
	if version != "" && !strings.Contains(version, "dev") {
		u := updater.NewUpdater(
			version,
			"ao-data",
			"albiondata-client",
			"update-",
		)

		go func() {
			maxTries := 2
			for i := 0; i < maxTries; i++ {
				err := u.BackgroundUpdater()
				if err != nil {
					log.Error(err.Error())
					log.Info("Will try again in 60 seconds. You may need to run the client as Administrator.")
					// Sleep and hope the network connects
					time.Sleep(time.Second * 60)
				} else {
					break
				}
			}
		}()
	}
}
