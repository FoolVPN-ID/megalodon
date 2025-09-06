package sandbox

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/FoolVPN-ID/megalodon/common/helper"
	fastshot "github.com/opus-domini/fast-shot"
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

	for _, connectivityTest := range connectivityTestList {
		httpClient := fastshot.NewClient(connectivityTest).
			Config().SetProxy(fmt.Sprintf("socks5://0.0.0.0:%d", int(freePort))).
			Config().SetTimeout(3 * time.Second).
			Build()

		resp, err := httpClient.GET("").Send()
		if err != nil {
			return configGeoip, err
		} else {
			if resp.Status().Code() == 200 {
				resp.Body().AsJSON(&configGeoip)
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
