package pkg

import (
	"fmt"
	"github.com/opslevel/opslevel-go"
	"github.com/spf13/cobra"
	"strings"
)

var _apiToken string
var _apiUrl string
var _userAgent string
var _apiVisibility string
var _clientGQL *opslevel.Client

func NewGraphClient(apiToken string, apiUrl string, userAgent string, apiVisibility string) *opslevel.Client {
	client := opslevel.NewClient(
		apiToken,
		opslevel.SetURL(apiUrl),
		opslevel.SetUserAgentExtra(userAgent),
		opslevel.SetAPIVisibility(apiVisibility),
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

func SetupGQLClient(apiToken string, apiUrl string, userAgent string, apiVisibility string) {
	_apiToken = apiToken
	_apiUrl = apiUrl
	_userAgent = userAgent
	_apiVisibility = apiVisibility
	_clientGQL = NewGraphClient(apiToken, apiUrl, userAgent, apiVisibility)
}

func GetGQLClient() *opslevel.Client {
	if _clientGQL == nil {
		_clientGQL = NewGraphClient(_apiToken, _apiUrl, _userAgent, _apiVisibility)
	}
	return _clientGQL
}
