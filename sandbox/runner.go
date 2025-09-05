package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/FoolVPN-ID/megalodon/common/helper"
	"github.com/Noooste/azuretls-client"
	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/option"
)

var orgPattern = regexp.MustCompile(`(\w*)`)
var connectivityTestList = []string{
	"https://myip.ipeek.workers.dev",
}

func testSingConfigWithContext(singConfig option.Options, ctx context.Context) (configGeoipStruct, error) {
	// Re-allocate free port
	var (
		freePort     = helper.GetFreePort()
		mixedOptions = singConfig.Inbounds[0].Options.(*option.HTTPMixedInboundOptions)
	)

	mixedOptions.ListenPort = uint16(freePort)
	singConfig.Inbounds[0].Options = mixedOptions

	configGeoip := configGeoipStruct{
		Country:        "XX",
		AsOrganization: "Megalodon",
	}
	boxInstance, err := box.New(box.Options{
		Context: ctx,
		Options: singConfig,
	})
	if err != nil {
		return configGeoip, err
	}

	// Start sing-box
	defer boxInstance.Close()
	if err := boxInstance.Start(); err != nil {
		return configGeoip, err
	}

	session := azuretls.NewSessionWithContext(ctx)
	session.InsecureSkipVerify = true
	defer session.Close()

	session.SetProxy(fmt.Sprintf("socks5://0.0.0.0:%v", freePort))

	for _, connectivityTest := range connectivityTestList {
		if err := session.Connect(connectivityTest); err != nil {
			return configGeoip, err
		}

		resp, err := session.Get(connectivityTest)
		if err != nil {
			return configGeoip, err
		} else {
			if resp.StatusCode == 200 {
				json.Unmarshal(resp.Body, &configGeoip)
			}
		}

		// Post-processing geoip
		filteredAsOrganization := orgPattern.FindAllString(configGeoip.AsOrganization, -1)
		configGeoip.AsOrganization = strings.Join(filteredAsOrganization, " ")

		if configGeoip.AsOrganization != "" && configGeoip.Country != "" {
			break
		}
	}

	return configGeoip, nil
}
