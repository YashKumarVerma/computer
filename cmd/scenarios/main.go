package main

import (
	"fmt"
	"os"
	"time"

	"github.com/yashkumarverma/computer/sdk"
)

func main() {
	client, err := sdk.Init(&sdk.Opts{
		Debug:         true,
		KeyValConnStr: "valkey://localhost:6379/0",
		InstrumentationCfg: &sdk.InstrumentationCfg{
			SentryDSN:   "https://fe3a825965596fc513b81acf1f26f1f3@sentry.fampay.co/159",
			TracingRate: 1,
			Timeout:     60 * time.Second,
		},
		ControlPlane: nil,
	})
	if err != nil {
		fmt.Println("error initializing sdk")
		os.Exit(1)
	}

	nsJuspay := client.RegisterNamespace("juspay")
	nsRzp := client.RegisterNamespace("razorpay")

	sendMoneyOperation := client.RegisterOperation(&sdk.NewOpsOpts{
		NameSpace:  nsJuspay,
		Name:       "sendMoney",
		EvalWindow: time.Minute * 1,
		Resolution: time.Second * 5,
	})

	walletLoadOperation := client.RegisterOperation(&sdk.NewOpsOpts{
		NameSpace:  nsRzp,
		Name:       "walletLoad",
		EvalWindow: time.Second * 60,
		Resolution: time.Second * 15,
	})

	//sendMoneyOperation.RecordSuccess()

	go client.RunMetricServer()
	time.Sleep(5 * time.Second)

	_, _ = sendMoneyOperation, walletLoadOperation
}
