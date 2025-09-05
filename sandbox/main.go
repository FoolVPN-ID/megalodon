package sandbox

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/FoolVPN-ID/megalodon/common/helper"
	logger "github.com/FoolVPN-ID/megalodon/log"
	"github.com/FoolVPN-ID/tool/modules/config"
	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json"
)

var (
	testTypes = []string{"cdn", "sni"}
	sniHost   = "meet.google.com"
	cdnHost   = "104.18.2.2"
)

type sandboxStruct struct {
	Results []TestResultStruct
	log     *logger.LoggerStruct
	ids     []string
	sync.Mutex
}

func MakeSandbox() *sandboxStruct {
	return &sandboxStruct{
		log: logger.MakeLogger(),
	}
}

func (sb *sandboxStruct) TestConfig(rawConfig string, accountIndex, accountTotal int) error {
	singConfig, err := config.BuildSingboxConfig(rawConfig)
	if err != nil {
		return err
	}

	// Generate and check md5
	var (
		outboundByte, _ = json.Marshal(singConfig.Outbounds[0].Options)
		outboundMd5     = helper.GetMD5FromString(string(outboundByte))
	)

	if slices.Contains(sb.ids, outboundMd5) {
		return errors.New("duplicate account detected")
	}
	sb.ids = append(sb.ids, outboundMd5)

	testResult := TestResultStruct{
		Outbound:  singConfig.Outbounds[0],
		RawConfig: base64.StdEncoding.EncodeToString([]byte(rawConfig)),
	}

	for _, testType := range testTypes {
		singConfigMapping := map[string]any{}
		singConfigByte, _ := json.Marshal(singConfig)
		json.Unmarshal(singConfigByte, &singConfigMapping)

		outbound := singConfigMapping["outbounds"].([]any)[0].(map[string]any)

		if testType == "cdn" {
			outbound["server"] = cdnHost
		} else {
			if outbound["tls"] != nil {
				outboundTLS := outbound["tls"].(map[string]any)
				if outboundTLS["enabled"] == true {
					outboundTLS["insecure"] = true
					outboundTLS["server_name"] = sniHost

					outbound["tls"] = outboundTLS
				}
			}

			if outbound["transport"] != nil {
				outboundTransport := outbound["transport"].(map[string]any)
				if outboundTransport["headers"] != nil {
					transportHeaders := outboundTransport["headers"].(map[string]any)
					if transportHeaders["Host"] != nil {
						transportHeaders["Host"] = sniHost
					}
					outboundTransport["headers"] = transportHeaders
				}
				if outboundTransport["host"] != nil {
					outboundTransport["host"] = sniHost
				}

				outbound["transport"] = outboundTransport
			}
		}

		singConfigMapping["outbounds"].([]any)[0] = outbound

		configForTest := option.Options{}
		configForTestByte, err := json.Marshal(singConfigMapping)
		if err != nil {
			return err
		}

		ctx := context.Background()
		ctx = box.Context(ctx, include.InboundRegistry(), include.OutboundRegistry(), include.EndpointRegistry(), include.DNSTransportRegistry(), include.ServiceRegistry())
		err = configForTest.UnmarshalJSONContext(ctx, configForTestByte)
		if err != nil {
			return err
		}

		// Close closure variable
		func(connMode string) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			ctx = box.Context(ctx, include.InboundRegistry(), include.OutboundRegistry(), include.EndpointRegistry(), include.DNSTransportRegistry(), include.ServiceRegistry())
			defer cancel()

			if configGeoip, err := testSingConfigWithContext(configForTest, ctx); err == nil {
				testResult.TestPassed = append(testResult.TestPassed, connMode)
				testResult.ConfigGeoip = configGeoip
				sb.log.Success(fmt.Sprintf("[%d/%d] [%d+%d] %v %s %s", accountIndex, accountTotal, len(sb.Results), len(testResult.TestPassed), testResult.TestPassed, configGeoip.Country, configGeoip.AsOrganization))
			} else {
				sb.log.Error(fmt.Sprintf("[%d/%d] %s", accountIndex, accountTotal, err.Error()))
			}
		}(testType)
	}

	if len(testResult.TestPassed) > 0 {
		sb.addResult(testResult)
	}

	return nil
}

func (sb *sandboxStruct) addResult(result TestResultStruct) {
	sb.Lock()
	defer sb.Unlock()
	sb.Results = append(sb.Results, result)
}
