package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"time"

	"github.com/yashkumarverma/computer/sdk"
)

func main() {
	client, err := sdk.Init(&sdk.Opts{
		Debug:         true,
		KeyValConnStr: "valkey://127.0.0.1:6379/0",
		InstrumentationCfg: &sdk.InstrumentationCfg{
			SentryDSN:   "https://fe3a825965596fc513b81acf1f26f1f3@sentry.fampay.co/159",
			TracingRate: 1,
			Timeout:     60 * time.Second,
		},
		ControlPlane: nil,
	})
	if err != nil {
		fmt.Println("error initializing sdk:", err)
		os.Exit(1)
	}
	defer client.Close()

	// namespaces are broad grouping of operations
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

	go client.RunMetricServer()

	// Drive steady traffic so /metrics and SuccessRate keep moving.
	go drive(sendMoneyOperation, 90)  // ~90% success
	go drive(walletLoadOperation, 70) // ~70% success

	// Print the computed rate every 5s so you can eyeball it against /metrics.
	ctx := context.Background()
	for range time.Tick(5 * time.Second) {
		sm, _ := sendMoneyOperation.SuccessRate(ctx)
		wl, _ := walletLoadOperation.SuccessRate(ctx)
		fmt.Printf("sendMoney=%.2f walletLoad=%.2f\n", sm, wl)
	}
}

// drive records events forever at ~pct% success, a handful per second. Each
// event is an independent Bernoulli trial, so outcomes aren't correlated in
// time and the measured rate stays close to pct across window boundaries.
func drive(op *sdk.Operation, pct int) {
	ctx := context.Background()
	for range time.Tick(200 * time.Millisecond) {
		var err error
		if rand.IntN(100) < pct { // independent Bernoulli trial, ~pct% success
			err = op.RecordSuccess(ctx, time.Now())
		} else {
			err = op.RecordFailure(ctx, time.Now())
		}
		if err != nil {
			fmt.Println("record error:", err)
		}
	}
}
