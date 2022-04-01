package pkg

import (
	"fmt"
	"strings"

	"github.com/opslevel/opslevel-go"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func NewGraphClient(version string) *opslevel.Client {
	client := opslevel.NewClient(viper.GetString("api-token"), opslevel.SetURL(viper.GetString("api-url")), opslevel.SetUserAgentExtra(fmt.Sprintf("opslevel-runner-%s", version)), opslevel.SetAPIVisibility("internal"))

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
