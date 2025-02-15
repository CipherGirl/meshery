package mesh

import (
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/layer5io/meshery/mesheryctl/internal/cli/root/config"
	"github.com/layer5io/meshery/mesheryctl/pkg/utils"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Operation is the common body type to be passed for Mesh Ops
type Operation struct {
	Adapter    string `json:"adapter"`
	CustomBody string `json:"customBody"`
	DeleteOp   string `json:"deleteOp"`
	Namespace  string `json:"namespace"`
	Query      string `json:"query"`
}

var spec string
var adapterURL string
var namespace string
var tokenPath string
var watch bool
var err error

// validateCmd represents the service mesh validation command
var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate conformance to service mesh standards",
	Args:  cobra.NoArgs,
	Long:  `Validate service mesh conformance to different standard specifications`,
	PreRunE: func(cmd *cobra.Command, args []string) error {
		if err != nil {
			return err
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {

		log.Infof("Starting service mesh validation...")

		mctlCfg, err := config.GetMesheryCtl(viper.GetViper())
		if err != nil {
			return errors.Wrap(err, "error processing config")
		}

		// sync
		syncPath := mctlCfg.GetBaseMesheryURL() + "/api/system/sync"
		method := "GET"

		client := &http.Client{}

		syncReq, err := http.NewRequest(method, syncPath, strings.NewReader(""))
		if err != nil {
			return err
		}

		syncReq.Header.Add("Content-Type", "application/x-www-form-urlencoded;charset=UTF-8")

		err = utils.AddAuthDetails(syncReq, tokenPath)
		if err != nil {
			return err
		}

		_, err = client.Do(syncReq)
		if err != nil {
			return err
		}

		s := utils.CreateDefaultSpinner("Validation started", "\nValidation complete")

		path := mctlCfg.GetBaseMesheryURL() + "/api/mesh/ops"
		method = "POST"

		data := url.Values{}
		data.Set("adapter", adapterURL)
		data.Set("customBody", "")
		data.Set("deleteOp", "")
		data.Set("namespace", "meshery")

		// Choose which specification to use for conformance test
		switch spec {
		case "smi":
			{
				data.Set("query", "smi_conformance")
				break
			}
		case "smp":
			{
				return errors.New("support for SMP coming in a future release")
			}
		case "istio-vet":
			{
				if adapterURL == "meshery-istio:10000" {
					data.Set("query", "istio-vet")
					break
				}
				return errors.New("only Istio supports istio-vet operation")
			}
		default:
			{
				return errors.New("specified specification not found or not yet supported")
			}
		}

		payload := strings.NewReader(data.Encode())

		req, err := http.NewRequest(method, path, payload)

		if err != nil {
			return err
		}
		req.Header.Add("Content-Type", "application/x-www-form-urlencoded;charset=UTF-8")

		err = utils.AddAuthDetails(req, tokenPath)
		if err != nil {
			return err
		}

		res, err := client.Do(req)
		if err != nil {
			return err
		}

		defer res.Body.Close()
		body, err := ioutil.ReadAll(res.Body)
		if err != nil {
			return err
		}

		log.Infof(string(body))

		if watch {
			log.Infof("Verifying Operation")
			s.Start()
			_, err = waitForValidateResponse(mctlCfg, "Smi conformance test")
			if err != nil {
				return errors.Wrap(err, "error verifying installation")
			}
			s.Stop()
		}

		return nil
	},
}

func init() {
	validateCmd.Flags().StringVarP(&spec, "spec", "s", "smi", "specification to be used for conformance test")
	_ = validateCmd.MarkFlagRequired("spec")
	validateCmd.Flags().StringVarP(&adapterURL, "adapter", "a", "meshery-osm:10010", "Adapter to use for validation")
	_ = validateCmd.MarkFlagRequired("adapter")
	validateCmd.Flags().StringVarP(&tokenPath, "tokenPath", "t", "", "Path to token for authenticating to Meshery API")
	_ = validateCmd.MarkFlagRequired("tokenPath")
	validateCmd.Flags().BoolVarP(&watch, "watch", "w", false, "Watch for events and verify operation (in beta testing)")
}

func waitForValidateResponse(mctlCfg *config.MesheryCtlConfig, query string) (string, error) {
	var wg sync.WaitGroup
	wg.Add(1)

	path := mctlCfg.GetBaseMesheryURL() + "/api/events?client=cli_validate"
	method := "GET"
	client := &http.Client{}
	req, err := http.NewRequest(method, path, nil)
	req.Header.Add("Accept", "text/event-stream")
	if err != nil {
		return "", err
	}

	err = utils.AddAuthDetails(req, tokenPath)
	if err != nil {
		return "", err
	}

	res, err := client.Do(req)
	if err != nil {
		return "", err
	}

	event, _ := utils.ConvertRespToSSE(res)

	//Run a goroutine to wait for the response
	go func() {
		for i := range event {
			log.Infof("Event :" + i.Data)
			if strings.Contains(i.Data, query) {
				wg.Done()
			}
		}
	}()

	//Run a goroutine to wait for time out of 20 mins
	go func() {
		time.Sleep(time.Second * 1200)
		err = errors.New("timeout")
		wg.Done()
	}()

	//Wait till any one of the goroutines ends and return
	wg.Wait()

	if err != nil {
		return "", err
	}

	return "", nil
}
