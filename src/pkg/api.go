package pkg

import (
	"fmt"
	"strings"

	"github.com/opslevel/opslevel-go/v2022"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func NewGraphClient(version string) *opslevel.Client {
	apiToken := viper.GetString("api-token")
	apiURL := viper.GetString("api-url")
	userAgent := fmt.Sprintf("opslevel-runner-%s", version)
	client := opslevel.NewGQLClient(
		opslevel.SetAPIToken(apiToken),
		opslevel.SetURL(apiURL),
		opslevel.SetUserAgentExtra(userAgent),
		opslevel.SetAPIVisibility("internal"),
	)

	clientErr := client.Validate()
	if clientErr != nil {
		if strings.Contains(clientErr.Error(), "Please provide a valid OpsLevel API token") {
			cobra.CheckErr(fmt.Errorf("%s via 'export OPSLEVEL_API_TOKEN=XXX' or '--api-token=XXX'", clientErr.Error()))
		} else {
			cobra.CheckErr(clientErr)
		}
	}
	cobra.CheckErr(clientErr)

	return client
}
