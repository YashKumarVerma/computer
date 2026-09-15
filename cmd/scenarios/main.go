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
		Dimensions: []string{"bank", "psp", "router"},
		Eval:       sendMoneyEval,
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
	// degraded (~60% vs ~95%) so the marginal slice actually diverges. Unlike
	// walletLoad, this emits a typed payload and lets the operation's Eval
	// derive the verdict — exercising the predicate + guard path.
	bankHealth := map[string]int{"bankA": 95, "bankB": 92, "bankC": 60}
	banks := []string{"bankA", "bankB", "bankC"}
	psps := []string{"psp1", "psp2"}
	routers := []string{"r1", "r2"}
	go driveEval(sendMoneyOperation, func() (sdk.Dims, interface{}) {
		bank := banks[rand.IntN(len(banks))]
		dims := sdk.Dims{
			"bank":   bank,
			"psp":    psps[rand.IntN(len(psps))],
			"router": routers[rand.IntN(len(routers))],
		}
		status := "success"
		if rand.IntN(100) >= bankHealth[bank] {
			status = "failed"
		}
		return dims, SendMoneyResp{Status: status}
	})

	// Print the overall failure rate plus the failure rate spread across every
	// registered dimension value every 5s — no hardcoded bank list, the SDK
	// enumerates the values it has seen. bankC should read visibly worse. The
	// same numbers are on /metrics as internal_operation_failure_rate.
	ctx := context.Background()
	for range time.Tick(5 * time.Second) {
		sm, _ := sendMoneyOperation.FailureRate(ctx)
		spread, _ := sendMoneyOperation.FailureRates(ctx)
		wl, _ := walletLoadOperation.FailureRate(ctx)
		fmt.Printf("sendMoney fail=%.2f %v  walletLoad fail=%.2f\n", sm, spread, wl)
	}
}

// SendMoneyResp is the downstream's typed response. sendMoneyEval is a
// code-verdict predicate over it (design doc §7.2, the proto/gRPC default): a
// wrong type or nil-deref panics, and the SDK's guard turns that into SUCCESS
// plus an eval-error metric rather than blocking traffic on our own bug.
type SendMoneyResp struct {
	Status string
	Code   int
}

func sendMoneyEval(v interface{}) bool {
	return v.(SendMoneyResp).Status == "success"
}

// driveEval is drive's payload-based twin: next() returns a payload the
// operation's Eval scores, instead of a precomputed bool.
func driveEval(op *sdk.Operation, next func() (sdk.Dims, interface{})) {
	ctx := context.Background()
	for range time.Tick(200 * time.Millisecond) {
		dims, payload := next()
		if err := op.Record(ctx, time.Now(), payload, dims); err != nil {
			fmt.Println("record error:", err)
		}
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
