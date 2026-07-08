package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"time"

	"cloud.google.com/go/bigtable"
	"github.com/alexflint/go-arg"
	"github.com/jamiealquiza/tachymeter"
	"github.com/maguec/metermaid"
	"github.com/schollz/progressbar/v3"
	"go.uber.org/ratelimit"
)

var ctx = context.Background()

var args struct {
	Project          string `help:"GCP Project to use" default:"" arg:"--project, -p, env:GOOGLE_CLOUD_PROJECT"`
	Instance         string `help:"BT Instance to use" default:"" arg:"--instance, -i, env:GOOGLE_BIGTABLE_INSTANCE"`
	ColumnFamily     string `help:"BT Column Family to use" default:"" arg:"--column-family, -f, env:GOOGLE_BIGTABLE_COLUMN_FAMILY"`
	Table            string `help:"BT Table to use" default:"" arg:"--table, -t, env:GOOGLE_BIGTABLE_TABLE"`
	RPS              int    `help:"Number of updates per second " default:"100" arg:"--rps, -r, env:BT_RPS"`
	Records          int    `help:"Total Number of records to write (legacy)" default:"10000" arg:"--records, env:BT_RECORDS"`
	RecordsPerWriter int    `help:"Number of transactions each writer generates" default:"10" arg:"--records-per-writer, -w, env:BT_RECORDS_PER_WRITER"`
	Threads          int    `help:"Number of threads to concurrent write" default:"10" arg:"--threads, -z, env:BT_THREADS"`
	ExtraColumns     int    `help:"Number of extra fields concurrent write" default:"0" arg:"--extra-fields, -e, env:BT_EXTRA_FIELDS"`
	Verbose          bool   `help:"Show verbose output" default:"false" arg:"--verbose, -v, env:BT_VERBOSE"`
	Stats            bool   `help:"Show latency stats" default:"true" arg:"--stats, env:BTS_STATS"`
}

func checkDirectAcess(projectID, instanceID string, verbose bool) bool {

	ctx2 := context.Background()
	appProfileID := "default"

	isDirectPath, err := bigtable.CheckDirectAccessSupported(ctx2, projectID, instanceID, appProfileID)
	if err != nil {
		log.Fatalf("DirectPath check failed: %v", err)
	}

	if isDirectPath {
		if verbose {
			log.Printf("DirectPath connectivity is active for %s/%s", projectID, instanceID)
		}
		return true
	} else {
		if verbose {
			log.Printf("DirectPath connectivity is NOT active for %s/%s", projectID, instanceID)
		}
	}
	return false
}

func sliceContains(list []string, target string) bool {
	for _, s := range list {
		if s == target {
			return true
		}
	}
	return false
}

func writeWorkerOrder(
	id int, recordsPerWriter int, results chan<- time.Duration, rl ratelimit.Limiter, verbose bool,
	mm *metermaid.Metermaid, project, instance, table, family string, extra int, bar *progressbar.ProgressBar) {

	if verbose {
		log.Printf("Starting write worker: %d (Generating %d records)\n", id, recordsPerWriter)
	}
	isDirectAccessSupported := checkDirectAcess(project, instance, false)
	clientConfig := bigtable.ClientConfig{
		DisableDirectAccess: !isDirectAccessSupported,
	}
	client, err := bigtable.NewClientWithConfig(ctx, project, instance, clientConfig)
	if err != nil {
		log.Fatalf("bigtable.DirectPathConnection: %v", err)
	}
	tbl := client.Open(table)

	var muts []*bigtable.Mutation

	// Create a local random generator for this worker to avoid locking overhead
	r := rand.New(rand.NewSource(time.Now().UnixNano() + int64(id)))

	// Thread-specific loop generating exactly `recordsPerWriter` transactions
	for j := 0; j < recordsPerWriter; j++ {
		rl.Take()

		muts = nil
		mut := bigtable.NewMutation()

		// Random date over the last month (30 days)
		nowMilli := time.Now().UnixMilli()
		thirtyDaysMilli := int64(30 * 24 * 60 * 60 * 1000)
		createdTs := nowMilli - r.Int63n(thirtyDaysMilli)
		reversedTs := int64(^uint64(0)>>1) - createdTs // Long.MAX_VALUE - timestamp

		// Random merchant ID between 1 and 5000
		merchantID := fmt.Sprintf("m_%d", r.Intn(5000)+1)
		orderID := fmt.Sprintf("ord_%d_%d", id, j)

		// Random transaction amount between 1 and 99
		amountStr := fmt.Sprintf("%d", r.Intn(99)+1)

		mut.Set(family, "amount", bigtable.Timestamp(createdTs*1000), []byte(amountStr))
		mut.Set(family, "status", bigtable.Timestamp(createdTs*1000), []byte("completed"))
		mut.Set(family, "created", bigtable.Timestamp(createdTs*1000), []byte(fmt.Sprintf("%d", createdTs)))

		for x := 0; x < extra; x++ {
			mut.Set(family, fmt.Sprintf("extra-%d", x), bigtable.ServerTime, []byte(fmt.Sprintf("%d", j)))
		}

		// Ensure mutation is appended to the slice before applying
		muts = append(muts, mut)

		startTime := time.Now()

		// Formatted row key: merchant_id#reversed_timestamp#order_id
		rowKeys := []string{fmt.Sprintf("%s#%019d#%s", merchantID, reversedTs, orderID)}

		if _, err := tbl.ApplyBulk(ctx, rowKeys, muts); err != nil {
			log.Fatalf("ApplyBulk Worker %d: iter %d: %v", id, j, err)
		}
		bar.Add(1)
		results <- time.Since(startTime)
		mm.Add()
	}
	client.Close()
}

func printResults(title string, tach *tachymeter.Tachymeter, mm *metermaid.Metermaid) {
	results := tach.Calc()
	fmt.Printf("------------------ %s Latency ------------------\n", title)
	fmt.Printf(
		"Max:\t\t%s\nMin:\t\t%s\nP95:\t\t%s\nP99:\t\t%s\nP99.9:\t\t%s\n\n",
		results.Time.Max,
		results.Time.Min,
		results.Time.P95,
		results.Time.P99,
		results.Time.P999,
	)
	fmt.Printf("-------------- %s Latency Histogram ------------\n", title)
	fmt.Println("")
	fmt.Println(results.Histogram.String(10))
	rates := mm.Calc()
	fmt.Printf("-------------------- %s Rate -------------------\n", title)
	fmt.Printf(
		"MaxRate:\t%f\nMinRate:\t%f\nP95Rate:\t%f\nP99Rate:\t%f\nP99.9Rate:\t%f\n",
		rates.MaxRate, rates.MinRate, rates.P95Rate, rates.P99Rate, rates.P999Rate,
	)
	fmt.Println("")
}

func main() {
	arg.MustParse(&args)

	if args.ColumnFamily == "" || args.Instance == "" || args.Project == "" || args.Table == "" {
		log.Fatal("Please specify project, instance, column family and table")
	}

	rl := ratelimit.New(args.RPS)

	// Calculate actual total records based on threads and records per writer
	totalRecords := args.Threads * args.RecordsPerWriter

	res := make(chan time.Duration, totalRecords)
	tach := tachymeter.New(&tachymeter.Config{Size: totalRecords})
	mm := metermaid.New(&metermaid.Config{Size: totalRecords})

	if args.Verbose {
		checkDirectAcess(args.Project, args.Instance, args.Verbose)
		log.Printf("Writing of %d records started", totalRecords)
	}

	bar := progressbar.Default(int64(totalRecords))

	for w := 1; w <= args.Threads; w++ {
		go writeWorkerOrder(w, args.RecordsPerWriter, res, rl, args.Verbose, mm, args.Project, args.Instance, args.Table, args.ColumnFamily, args.ExtraColumns, bar)
	}

	// Read totalRecords dynamically instead of args.Records
	for a := 0; a < totalRecords; a++ {
		r := <-res
		tach.AddTime(r)
	}

	if args.Verbose {
		log.Printf("Writing of %d records complete", totalRecords)
	}

	printResults("Writer", tach, mm)
}