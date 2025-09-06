package provider

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/FoolVPN-ID/megalodon/common/helper"
	"github.com/FoolVPN-ID/megalodon/constant"
	fastshot "github.com/opus-domini/fast-shot"
)

var configSeparators = []string{"\n", "|", ",", "<br/>"}

func (prov *providerStruct) GatherSubFile() {
	var subFileUrlString, err = helper.ReadFileAsString("./resources/sublist.json")
	var subFileUrls = []string{}

	if err != nil {
		prov.logger.Error(err.Error())
		return
	}

	json.Unmarshal([]byte(subFileUrlString), &subFileUrls)

	for _, subFileUrl := range subFileUrls {
		func() {
			resp, err := fastshot.NewClient(subFileUrl).
				Config().SetTimeout(10 * time.Second).
				Build().GET("").Send()

			if err != nil {
				prov.logger.Error(err.Error())
				return
			}

			if resp.Status().Code() == 200 {
				var subFile = []providerSubStruct{}
				if err := resp.Body().AsJSON(&subFile); err == nil {
					prov.subs = append(prov.subs, subFile...)
				}
			}
		}()
	}
}

func (prov *providerStruct) GatherNodes() {
	var (
		wg    = sync.WaitGroup{}
		queue = make(chan struct{}, 10)
	)

	for i, sub := range prov.subs {
		var subUrls = strings.Split(sub.URL, "|")
		for x, subUrl := range subUrls {
			wg.Add(1)
			queue <- struct{}{}

			go (func() {
				defer func() {
					wg.Done()
					<-queue
				}()
				defer func() {
					recover()
				}()

				resp, err := fastshot.NewClient(subUrl).
					Config().SetTimeout(10 * time.Second).
					Build().GET("").Send()
				if err != nil {
					panic(err)
				}

				if resp.Status().Code() == 200 {
					var (
						nodes       = []string{}
						textBody, _ = resp.Body().AsString()
					)

					if len(textBody) < 100 {
						return
					}

					if !strings.Contains(textBody, "://") {
						parsedBody := helper.DecodeBase64Safe(textBody)
						if parsedBody == textBody {
							if parsedBodyByte, err := base64.StdEncoding.DecodeString(textBody); err == nil {
								parsedBody = string(parsedBodyByte)
							} else {
								if parsedBodyByte, err = base64.RawStdEncoding.DecodeString(textBody); err == nil {
									parsedBody = string(parsedBodyByte)
								} else {
									prov.logger.Error(err.Error())
								}
							}
						}

						textBody = parsedBody
					}

					for _, separator := range configSeparators {
						nodes = append(nodes, strings.Split(textBody, separator)...)
					}

					var addedNodesCount = 0
					for _, node := range nodes {
						for _, acceptedType := range constant.ACCEPTED_TYPES {
							if strings.HasPrefix(node, acceptedType) {
								addedNodesCount += 1
								prov.addNode(node)
							}
						}
					}

					prov.logger.Info(fmt.Sprintf("[[%d/%d]%d/%d] [%d] [%d] %s\n", x, len(subUrls), i, len(prov.subs), addedNodesCount, len(prov.Nodes), subUrl))
				}
			})()
		}
	}

	// Wait for all goroutines
	wg.Wait()
}

func (prov *providerStruct) addNode(node string) {
	prov.Lock()
	defer prov.Unlock()

	if !slices.Contains(prov.Nodes, node) {
		prov.Nodes = append(prov.Nodes, node)
	}
}
