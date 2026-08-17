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
		// Closed set of slices we're allowed to break failure rate down by.
		Dimensions: []string{"bank", "psp", "router"},
	})

	walletLoadOperation := client.RegisterOperation(&sdk.NewOpsOpts{
		NameSpace:  nsRzp,
		Name:       "walletLoad",
		EvalWindow: time.Second * 60,
		Resolution: time.Second * 15,
	})

	go client.RunMetricServer()

	// walletLoad: plain ~70% success, no dimensions.
	go drive(walletLoadOperation, func() (sdk.Dims, bool) {
		return nil, rand.IntN(100) < 70
	})

	// sendMoney: every attempt routes through a bank/psp/router; bankC is
	// degraded (~60% vs ~95%) so the marginal slice actually diverges.
	bankHealth := map[string]int{"bankA": 95, "bankB": 92, "bankC": 60}
	banks := []string{"bankA", "bankB", "bankC"}
	psps := []string{"psp1", "psp2"}
	routers := []string{"r1", "r2"}
	go drive(sendMoneyOperation, func() (sdk.Dims, bool) {
		bank := banks[rand.IntN(len(banks))]
		dims := sdk.Dims{
			"bank":   bank,
			"psp":    psps[rand.IntN(len(psps))],
			"router": routers[rand.IntN(len(routers))],
		}
		return dims, rand.IntN(100) < bankHealth[bank]
	})

	// Print overall + the per-bank slices every 5s. bankC should read visibly
	// lower than the overall sendMoney rate.
	ctx := context.Background()
	for range time.Tick(5 * time.Second) {
		sm, _ := sendMoneyOperation.SuccessRate(ctx)
		a, _ := sendMoneyOperation.SuccessRateBy(ctx, "bank", "bankA")
		b, _ := sendMoneyOperation.SuccessRateBy(ctx, "bank", "bankB")
		c, _ := sendMoneyOperation.SuccessRateBy(ctx, "bank", "bankC")
		wl, _ := walletLoadOperation.SuccessRate(ctx)
		fmt.Printf("sendMoney=%.2f [bankA=%.2f bankB=%.2f bankC=%.2f]  walletLoad=%.2f\n",
			sm, a, b, c, wl)
	}
}

// drive records events forever, a handful per second. next() returns the
// dimensions to tag each event with and whether it succeeded; each call is
// independent so the measured rate stays close to target across window edges.
func drive(op *sdk.Operation, next func() (sdk.Dims, bool)) {
	ctx := context.Background()
	for range time.Tick(200 * time.Millisecond) {
		dims, success := next()
		var err error
		if success {
			err = op.RecordSuccess(ctx, time.Now(), dims)
		} else {
			err = op.RecordFailure(ctx, time.Now(), dims)
		}
		if err != nil {
			fmt.Println("record error:", err)
		}
	}
}
