package main

import (
	"context"
	"fmt"
	"log"
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
	Project      string `help:"GCP Project to use" default:"" arg:"--project, -p, env:GOOGLE_CLOUD_PROJECT"`
	Instance     string `help:"BT Instance to use" default:"" arg:"--instance, -i, env:GOOGLE_BIGTABLE_INSTANCE"`
	ColumnFamily string `help:"BT Column Family to use" default:"" arg:"--column-family, -f, env:GOOGLE_BIGTABLE_COLUMN_FAMILY"`
	Table        string `help:"BT Table to use" default:"" arg:"--table, -t, env:GOOGLE_BIGTABLE_TABLE"`
	RPS          int    `help:"Number of updates per second " default:"100" arg:"--rps, -r, env:BT_RPS"`
	Records      int    `help:"Toal Number of records to write" default:"10000" arg:"--records, -w, env:BT_RECORDS"`
	Threads      int    `help:"Number of threads to concurrent write" default:"10" arg:"--threads, -z, env:BT_THREADS"`
	Verbose      bool   `help:"Show verbose output" default:"false" arg:"--verbose, -v, env:BT_VERBOSE"`
	Stats        bool   `help:"Show latency stats" default:"true" arg:"--stats, -z, env:BTS_STATS"`
}

func sliceContains(list []string, target string) bool {
	for _, s := range list {
		if s == target {
			return true
		}
	}
	return false
}

func createTable(project, instance, table, columnFamily string) error {
	adminClient, err := bigtable.NewAdminClient(ctx, project, instance)
	if err != nil {
		return fmt.Errorf("Admin Client(%s:%s): %v", project, instance, err)
	}

	tables, err := adminClient.Tables(ctx)
	if err != nil {
		return fmt.Errorf("List Tables(%s): %v", instance, err)
	}

	if !sliceContains(tables, table) {
		log.Printf("Creating table %s", table)
		if err := adminClient.CreateTable(ctx, table); err != nil {
			return fmt.Errorf("CreateTable(%s): %v", table, err)
		}
		if err := adminClient.CreateColumnFamily(ctx, table, columnFamily); err != nil {
			return fmt.Errorf("CreateColumnFamily(%s): %v", columnFamily, err)
		}
		maxAge := time.Hour * 24
		policy := bigtable.MaxAgePolicy(maxAge)
		if err := adminClient.SetGCPolicy(ctx, table, columnFamily, policy); err != nil {
			return fmt.Errorf("SetGCPolicy(%s): %v", policy, err)
		}
	}

	return nil
}

func writeWorker(
	id int, jobs <-chan int, results chan<- time.Duration, rl ratelimit.Limiter, verbose bool,
	mm *metermaid.Metermaid, project, instance, table, family string, bar *progressbar.ProgressBar) {

	if verbose {
		log.Printf("Starting write worker: %d\n", id)
	}
	client, err := bigtable.NewClient(ctx, project, instance)
	if err != nil {
		log.Fatalf("bigtable.NewAdminClient: %v", err)
	}
	tbl := client.Open(table)

	var muts []*bigtable.Mutation
	for j := range jobs {
		rl.Take()
		startTime := time.Now()

		muts = nil
		mut := bigtable.NewMutation()
		mut.Set(family, "pop", bigtable.Timestamp(startTime.UnixMicro()), []byte(fmt.Sprintf("%d", j)))
		mut.Set(family, "write", bigtable.ServerTime, []byte(fmt.Sprintf("%d", j)))
		muts = append(muts, mut)

		rowKeys := []string{fmt.Sprintf("user#user-%d", j)}
		if _, err := tbl.ApplyBulk(ctx, rowKeys, muts); err != nil {
			log.Fatalf("ApplyBulk: %d: %v", j, err)
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

func verifyServerLatencies(project, instance, table, family string, expectedRecords int, bar *progressbar.ProgressBar) {
	log.Println("Scanning table to verify server-side latencies...")
	client, err := bigtable.NewClient(ctx, project, instance)
	if err != nil {
		log.Fatalf("bigtable.NewClient: %v", err)
	}
	defer client.Close()

	tbl := client.Open(table)

	// Create a new tachymeter to measure the internal database latency
	tach := tachymeter.New(&tachymeter.Config{Size: expectedRecords})

	// Scan rows with the prefix we used during writing
	err = tbl.ReadRows(ctx, bigtable.PrefixRange("user#"), func(row bigtable.Row) bool {
		var popTS, writeTS bigtable.Timestamp

		// Iterate through the columns in the specified family
		for _, item := range row[family] {
			if item.Column == family+":pop" {
				popTS = item.Timestamp
			} else if item.Column == family+":write" {
				writeTS = item.Timestamp
			}
		}

		// If both timestamps exist, calculate the delta
		if popTS > 0 && writeTS > 0 {
			// Bigtable Timestamps are in microseconds.
			// We subtract them and convert the difference to time.Duration
			diffMicro := int64(writeTS - popTS)
			tach.AddTime(time.Duration(diffMicro) * time.Microsecond)
		}
		bar.Add(1)

		return true // Return true to continue scanning the next row
	})

	if err != nil {
		log.Fatalf("ReadRows encountered an error: %v", err)
	}

	// Calculate and print the server-side results
	results := tach.Calc()
	fmt.Printf("---------------- Server-Side Latency (Write - Pop) ----------------\n")
	fmt.Printf(
		"Max:\t\t%s\nMin:\t\t%s\nP95:\t\t%s\nP99:\t\t%s\nP99.9:\t\t%s\n\n",
		results.Time.Max,
		results.Time.Min,
		results.Time.P95,
		results.Time.P99,
		results.Time.P999,
	)
}

func main() {
	arg.MustParse(&args)
	err := createTable(
		args.Project,
		args.Instance,
		args.Table,
		args.ColumnFamily,
	)

	if args.ColumnFamily == "" || args.Instance == "" || args.Project == "" || args.Table == "" {
		log.Fatal("Please specificy project, instance, column family and table")
	}

	if err != nil {
		log.Fatalf("Could not create admin client: %v", err)
	}

	rl := ratelimit.New(args.RPS)
	jobs := make(chan int, args.Records)
	res := make(chan time.Duration, args.Records)
	tach := tachymeter.New(&tachymeter.Config{Size: args.Records})
	mm := metermaid.New(&metermaid.Config{Size: args.Records})

	for i := 0; i < args.Records; i++ {
		jobs <- i
	}

	if args.Verbose {
		log.Printf("Writing of %d records started", args.Records)
	}

	bar := progressbar.Default(int64(args.Records))

	for w := 1; w <= args.Threads; w++ {
		go writeWorker(w, jobs, res, rl, args.Verbose, mm, args.Project, args.Instance, args.Table, args.ColumnFamily, bar)
	}

	for a := 0; a < args.Records; a++ {
		r := <-res
		tach.AddTime(r)
	}

	if args.Verbose {
		log.Printf("Writing of %d records complete", args.Records)
	}

	printResults("Writer", tach, mm)

	bar2 := progressbar.Default(int64(args.Records))
	verifyServerLatencies(args.Project, args.Instance, args.Table, args.ColumnFamily, args.Records, bar2)

}
