package pkg

import (
	"fmt"
	"github.com/go-resty/resty/v2"
	"strings"
	"time"

	"github.com/opslevel/opslevel-go/v2023"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var _version string
var _clientRest *resty.Client
var _clientGQL *opslevel.Client

func SetClientVersion(version string) {
	_version = version
}

func NewRestClient() *resty.Client {
	if _clientRest == nil {
		_clientRest = opslevel.NewRestClient(opslevel.SetURL(viper.GetString("api-url")))
	}
	return _clientRest
}

func NewGraphClient() *opslevel.Client {
	if _clientGQL == nil {
		_clientGQL = newGraphClient()
	}
	return _clientGQL
}

func newGraphClient() *opslevel.Client {
	apiToken := viper.GetString("api-token")
	apiURL := viper.GetString("api-url")
	userAgent := fmt.Sprintf("opslevel-runner-%s", _version)
	client := opslevel.NewGQLClient(
		opslevel.SetAPIToken(apiToken),
		opslevel.SetURL(apiURL),
		opslevel.SetUserAgentExtra(userAgent),
		opslevel.SetMaxRetries(5),
		opslevel.SetTimeout(60*time.Second),
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
