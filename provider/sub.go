package provider

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/FoolVPN-ID/megalodon/common/helper"
	"github.com/FoolVPN-ID/megalodon/constant"
	"github.com/Noooste/azuretls-client"
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
			session := azuretls.NewSession()
			defer session.Close()

			resp, err := session.Get(subFileUrl)
			if err != nil {
				prov.logger.Error(err.Error())
				return
			}

			if resp.StatusCode == 200 {
				var subFile = []providerSubStruct{}
				if err := json.Unmarshal(resp.Body, &subFile); err == nil {
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

			prov.logger.Info(fmt.Sprintf("[[%d/%d]%d/%d] [%d] %s\n", x, len(subUrls), i, len(prov.subs), len(prov.Nodes), subUrl))
			go (func() {
				defer func() {
					wg.Done()
					<-queue
				}()
				defer func() {
					recover()
				}()

				session := azuretls.NewSession()
				session.SetTimeout(10 * time.Second)
				defer session.Close()

				resp, err := session.Get(subUrl)
				if err != nil {
					panic(err)
				}

				if resp.StatusCode == 200 {
					nodes := []string{}
					// re-Filter nodes due to some bullshit
					for _, separator := range configSeparators {
						if len(nodes) == 0 {
							nodes = append(nodes, strings.Split(helper.DecodeBase64Safe(string(resp.Body)), separator)...)
						} else {
							filteredNodes := []string{}
							for _, node := range nodes {
								filteredNodes = append(filteredNodes, strings.Split(node, separator)...)
							}

							nodes = filteredNodes
						}
					}

					for _, node := range nodes {
						for _, acceptedType := range constant.ACCEPTED_TYPES {
							if strings.HasPrefix(node, acceptedType) {
								prov.addNode(node)
							}
						}
					}
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
