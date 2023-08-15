/*
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"go/format"
	"log"
	"os"
	"runtime"
	"runtime/pprof"
	"sort"
	"time"

	"github.com/gpu-vmprovisioner/pkg/providers/pricing"
	"github.com/samber/lo"
)

var regions = []string{
	"australiacentral",
	"australiacentral2",
	"australiaeast",
	"australiasoutheast",
	"brazilsouth",
	"brazilsoutheast",
	"canadacentral",
	"canadaeast",
	"centralindia",
	"centralus",
	"eastasia",
	"eastus",
	"eastus2",
	"francecentral",
	"francesouth",
	"germanynorth",
	"germanywestcentral",
	"japaneast",
	"japanwest",
	"jioindiacentral",
	"jioindiawest",
	"koreacentral",
	"koreasouth",
	"northcentralus",
	"northeurope",
	"norwayeast",
	"norwaywest",
	"polandcentral",
	"qatarcentral",
	"southafricanorth",
	"southafricawest",
	"southcentralus",
	"southeastasia",
	"southindia",
	"swedencentral",
	"swedensouth",
	"switzerlandnorth",
	"switzerlandwest",
	"uaecentral",
	"uaenorth",
	"uksouth",
	"ukwest",
	"westcentralus",
	"westeurope",
	"westindia",
	"westus",
	"westus2",
	"westus3",
}

func main() {
	flag.Parse()
	if flag.NArg() != 1 {
		log.Fatalf("Usage: %s pkg/providers/pricing/zz_generated.pricing.go", os.Args[0])
	}

	generatePricing(flag.Arg(0))
}

func generatePricing(filePath string) {
	f, err := os.Create("pricing.heapprofile")
	if err != nil {
		log.Fatal("could not create memory profile: ", err)
	}
	defer f.Close() // error handling omitted for example

	ctx := context.Background()
	updateStarted := time.Now()
	src := &bytes.Buffer{}
	fmt.Fprintln(src, "//go:build !ignore_autogenerated")
	license := lo.Must(os.ReadFile("hack/boilerplate.go.txt"))
	fmt.Fprintln(src, string(license))
	fmt.Fprintln(src, "package pricing")
	fmt.Fprintln(src, `import "time"`)
	now := time.Now().UTC().Format(time.RFC3339)
	fmt.Fprintf(src, "// generated at %s\n\n\n", now)
	fmt.Fprintf(src, "var initialPriceUpdate, _ = time.Parse(time.RFC3339, \"%s\")\n", now)
	fmt.Fprintln(src, "var initialOnDemandPrices = map[string]map[string]float64{}")
	fmt.Fprintln(src, "func init() {")
	// record prices for each region
	var pricingProviderByRegion = map[string]chan *pricing.Provider{}
	for _, region := range regions {
		resultsChan := make(chan *pricing.Provider)
		log.Println("fetching pricing data in region", region)
		go func(region string, resultsChan chan *pricing.Provider) {
			pricingProvider := pricing.NewProvider(ctx, pricing.NewAPI(), region, make(chan struct{}))
			attempts := 0
			for {
				if pricingProvider.OnDemandLastUpdated().After(updateStarted) {
					break
				}

				if attempts == 0 {
					log.Println("started wait loop for pricing update on region", region)
				} else if attempts%10 == 0 {
					log.Printf("waiting on pricing update on region %s...\n", region)
				} else if time.Now().Sub(updateStarted) >= time.Minute*5 {
					log.Fatalf("failed to update region %s within 2 minutes", region)
				}
				time.Sleep(1 * time.Second)
				attempts += 1
			}
			log.Printf("fetched pricing for region %s\n", region)
			resultsChan <- pricingProvider
		}(region, resultsChan)
		pricingProviderByRegion[region] = resultsChan
	}
	for _, region := range regions {
		pricingProviderChan := pricingProviderByRegion[region]
		var pricingProvider *pricing.Provider = <-pricingProviderChan
		log.Println("writing output for", region)
		instanceTypes := pricingProvider.InstanceTypes()
		sort.Strings(instanceTypes)

		writePricing(src, instanceTypes, region, pricingProvider.OnDemandPrice)
	}
	fmt.Fprintln(src, "}")
	formatted, err := format.Source(src.Bytes())
	if err != nil {
		if err := os.WriteFile(filePath, src.Bytes(), 0644); err != nil {
			log.Fatalf("writing output, %s", err)
		}
		log.Fatalf("formatting generated source, %s", err)
	}

	if err := os.WriteFile(filePath, formatted, 0644); err != nil {
		log.Fatalf("writing output, %s", err)
	}
	runtime.GC()
	if err := pprof.WriteHeapProfile(f); err != nil {
		log.Fatal("could not write memory profile: ", err)
	}
	log.Printf("successfully generated pricing file: \"%s\"\n", filePath)
}

func writePricing(src *bytes.Buffer, instanceNames []string, region string, getPrice func(instanceType string) (float64, bool)) {
	fmt.Fprintf(src, "// %s\n", region)
	fmt.Fprintf(src, "initialOnDemandPrices[%q] = map[string]float64{\n", region)
	sort.Strings(instanceNames)
	for _, instanceName := range instanceNames {
		price, ok := getPrice(instanceName)
		if !ok {
			continue
		}

		// TODO: look at grouping by families to make the generated output nicer:
		// https://github.com/Azure/karpenter/pull/94#discussion_r1120901524
		_, err := fmt.Fprintf(src, "\"%s\":%f, \n", instanceName, price)
		if err != nil {
			log.Fatalf("error writing, %s", err)
		}
	}
	fmt.Fprintln(src, "\n}")
}
