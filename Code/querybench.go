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
	Project             string `help:"GCP Project to use" default:"" arg:"--project, -p, env:GOOGLE_CLOUD_PROJECT"`
	Instance            string `help:"BT Instance to use" default:"" arg:"--instance, -i, env:GOOGLE_BIGTABLE_INSTANCE"`
	QueryType           string `help:"Query type to run: daily, lifetime, date-bounded, top10" default:"daily" arg:"--query, -q, env:BT_QUERY_TYPE"`
	MerchantID          string `help:"Merchant ID to query (if empty, a random one m_1 to m_5000 is chosen per execution)" default:"" arg:"--merchant, -m, env:BT_MERCHANT_ID"`
	Runs                int    `help:"Total number of query executions to run" default:"100" arg:"--runs, -n, env:BT_RUNS"`
	Threads             int    `help:"Number of concurrent threads running queries" default:"5" arg:"--threads, -z, env:BT_THREADS"`
	RPS                 int    `help:"Rate limit of queries per second across all threads" default:"10" arg:"--rps, -r, env:BT_RPS"`
	Verbose             bool   `help:"Show verbose output and printed query results" default:"false" arg:"--verbose, -v, env:BT_VERBOSE"`
	Stats               bool   `help:"Show latency stats" default:"true" arg:"--stats, env:BTS_STATS"`
	DisableDirectAccess bool   `help:"Disable Direct Access even if available" default:"false" arg:"--disable-direct-access, -d, env:BTS_DISABLE_DIRECT_ACCESS"`
}

func checkDirectAccess(projectID, instanceID string, verbose, disableDirectAccess bool) bool {
	if disableDirectAccess {
		if verbose {
			log.Printf("DirectPath short circuited by flag for %s/%s", projectID, instanceID)
		}
		return false
	}

	ctx2 := context.Background()
	appProfileID := "default"

	isDirectPath, err := bigtable.CheckDirectAccessSupported(ctx2, projectID, instanceID, appProfileID)
	if err != nil {
		log.Fatalf("DirectPath check failed: %v", err)
	}

	if isDirectPath && !disableDirectAccess {
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

func queryWorker(
	id int, runsToProcess int, results chan<- time.Duration, rl ratelimit.Limiter, verbose bool,
	mm *metermaid.Metermaid, project, instance, queryType, merchantID string, bar *progressbar.ProgressBar) {

	if verbose {
		log.Printf("Starting query worker: %d (Running %d queries of type %s)\n", id, runsToProcess, queryType)
	}
	isDirectAccessSupported := checkDirectAccess(project, instance, false, args.DisableDirectAccess)
	clientConfig := bigtable.ClientConfig{
		DisableDirectAccess: !isDirectAccessSupported,
	}
	client, err := bigtable.NewClientWithConfig(ctx, project, instance, clientConfig)
	if err != nil {
		log.Fatalf("bigtable.NewClientWithConfig: %v", err)
	}
	defer client.Close()

	var queryStr string
	var paramTypes map[string]bigtable.SQLType

	// Determine query template and parameters based on queryType
	switch queryType {
	case "daily":
		queryStr = "SELECT * FROM orders_daily_metrics_mv WHERE merchant_id = @merchantID ORDER BY day DESC"
		paramTypes = map[string]bigtable.SQLType{
			"merchantID": bigtable.StringSQLType{},
		}
	case "lifetime":
		queryStr = "SELECT merchant_id, SUM(total_amount) AS lifetime_volume, SUM(order_count) AS lifetime_orders FROM orders_daily_metrics_mv WHERE merchant_id = @merchantID GROUP BY merchant_id"
		paramTypes = map[string]bigtable.SQLType{
			"merchantID": bigtable.StringSQLType{},
		}
	case "date-bounded":
		queryStr = "SELECT merchant_id, SUM(total_amount) AS window_volume FROM orders_daily_metrics_mv WHERE merchant_id = @merchantID AND day >= '2026-07-01 00:00:00' GROUP BY merchant_id"
		paramTypes = map[string]bigtable.SQLType{
			"merchantID": bigtable.StringSQLType{},
		}
	case "top10":
		queryStr = `SELECT 
         merchant_id, 
         SUM(total_amount) AS window_volume 
       FROM orders_daily_metrics_mv 
       WHERE day >= '2026-07-01 00:00:00' AND day < '2026-08-01 00:00:00' 
       GROUP BY merchant_id 
       ORDER BY window_volume DESC 
       LIMIT 10`
		paramTypes = nil
	default:
		log.Fatalf("Unknown query type: %s. Choose from daily, lifetime, date-bounded, top10", queryType)
	}

	// Prepare the statement once for the worker
	ps, err := client.PrepareStatement(ctx, queryStr, paramTypes)
	if err != nil {
		log.Fatalf("PrepareStatement: %v", err)
	}

	r := rand.New(rand.NewSource(time.Now().UnixNano() + int64(id)))

	for j := 0; j < runsToProcess; j++ {
		rl.Take()

		// Bind parameters
		var bs *bigtable.BoundStatement
		if paramTypes != nil {
			mID := merchantID
			if mID == "" {
				mID = fmt.Sprintf("m_%d", r.Intn(5000)+1)
			}
			var bindErr error
			bs, bindErr = ps.Bind(map[string]any{
				"merchantID": mID,
			})
			if bindErr != nil {
				log.Fatalf("Bind: %v", bindErr)
			}
		} else {
			var bindErr error
			bs, bindErr = ps.Bind(nil)
			if bindErr != nil {
				log.Fatalf("Bind: %v", bindErr)
			}
		}

		startTime := time.Now()

		var rowsCount int
		// Execute query and consume results
		err = bs.Execute(ctx, func(row bigtable.ResultRow) bool {
			rowsCount++
			if verbose {
				// Accessing column data
				switch queryType {
				case "daily":
					var m string
					var total int64
					var cnt int64
					_ = row.GetByName("merchant_id", &m)
					_ = row.GetByName("total_amount", &total)
					_ = row.GetByName("order_count", &cnt)
					// Note: 'day' can be printed as its default raw representation if we don't bind to a specific type
					log.Printf("[Worker %d] Daily MV Row: merchant=%s total=%d count=%d", id, m, total, cnt)
				case "lifetime":
					var m string
					var total int64
					var cnt int64
					_ = row.GetByName("merchant_id", &m)
					_ = row.GetByName("lifetime_volume", &total)
					_ = row.GetByName("lifetime_orders", &cnt)
					log.Printf("[Worker %d] Lifetime Rollup: merchant=%s volume=%d orders=%d", id, m, total, cnt)
				case "date-bounded":
					var m string
					var windowVol int64
					_ = row.GetByName("merchant_id", &m)
					_ = row.GetByName("window_volume", &windowVol)
					log.Printf("[Worker %d] Date-Bounded: merchant=%s window_volume=%d", id, m, windowVol)
				case "top10":
					var m string
					var windowVol int64
					_ = row.GetByName("merchant_id", &m)
					_ = row.GetByName("window_volume", &windowVol)
					log.Printf("[Worker %d] Top 10 Row: merchant=%s window_volume=%d", id, m, windowVol)
				}
			}
			return true // Continue scanning rows
		})

		if err != nil {
			log.Fatalf("Execute query failed (Worker %d, iter %d): %v", id, j, err)
		}

		duration := time.Since(startTime)
		bar.Add(1)
		results <- duration
		mm.Add()
	}
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

	if args.Instance == "" || args.Project == "" {
		log.Fatal("Please specify project and instance using flags or environment variables")
	}

	rl := ratelimit.New(args.RPS)

	// Calculate the distribution of runs per thread
	totalRuns := args.Runs
	basePerWriter := totalRuns / args.Threads
	remainder := totalRuns % args.Threads

	// Cap tachymeter size
	tachSize := totalRuns
	if tachSize > 1000000 {
		tachSize = 1000000
	}

	res := make(chan time.Duration, 100000)
	tach := tachymeter.New(&tachymeter.Config{Size: tachSize})
	mm := metermaid.New(&metermaid.Config{Size: totalRuns})

	if args.Verbose {
		checkDirectAccess(args.Project, args.Instance, args.Verbose, args.DisableDirectAccess)
		log.Printf("Query benchmark of %d runs started using %d threads", totalRuns, args.Threads)
	}

	bar := progressbar.Default(int64(totalRuns))

	for w := 1; w <= args.Threads; w++ {
		runsForThisWriter := basePerWriter
		if w <= remainder {
			runsForThisWriter++
		}

		go queryWorker(w, runsForThisWriter, res, rl, args.Verbose, mm, args.Project, args.Instance, args.QueryType, args.MerchantID, bar)
	}

	// Process the results as they stream in
	for a := 0; a < totalRuns; a++ {
		r := <-res
		tach.AddTime(r)
	}

	if args.Verbose {
		log.Printf("Query benchmark of %d runs complete", totalRuns)
	}

	printResults("Query Bench (" + args.QueryType + ")", tach, mm)
}
