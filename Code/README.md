# Setup

## Setup for orders

### Enable the necessary tables

```bash
# 1. Create the raw orders table
cbt -project $GOOGLE_CLOUD_PROJECT -instance $GOOGLE_BIGTABLE_INSTANCE createtable orders
cbt -project $GOOGLE_CLOUD_PROJECT -instance $GOOGLE_BIGTABLE_INSTANCE createfamily orders core

# 2. Create the destination CMV table
cbt -project $GOOGLE_CLOUD_PROJECT -instance $GOOGLE_BIGTABLE_INSTANCE createtable orders_daily_metrics_mv
cbt -project $GOOGLE_CLOUD_PROJECT -instance $GOOGLE_BIGTABLE_INSTANCE createfamily orders_daily_metrics_mv aggregates

# 3. Apply the CMV definition (Conceptual representation of the native BT CMV binding)
gcloud bigtable materialized-views create orders_daily_metrics_mv \
  --instance=${GOOGLE_BIGTABLE_INSTANCE} \
  --query="
    SELECT 
      SPLIT(CAST(_key AS STRING), '#')[OFFSET(0)] AS merchant_id, 
      TIMESTAMP_TRUNC(TIMESTAMP_MILLIS(CAST(CAST(core['created'] AS STRING) AS INT64)), DAY) AS day, 
      SUM(CAST(CAST(core['amount'] AS STRING) AS INT64)) AS total_amount, 
      COUNT(*) AS order_count 
    FROM orders 
    WHERE _key IS NOT NULL AND core['created'] IS NOT NULL 
    GROUP BY 1, 2
  "
```


### Add some records

The following will bring a single node bigtable instance to 100% CPU usage
```bash
go run seedorders.go --project ${GOOGLE_CLOUD_PROJECT} --instance ${GOOGLE_BIGTABLE_INSTANCE} --column-family=core --table orders --records 10000000  -r 10000 -z 250
```

check the Count
```bash
cbt -project ${GOOGLE_CLOUD_PROJECT} -instance bt-i-f29db430f6487ba7 count orders
```


### Read from the CMV

Fetch Daily Metrics for a Specific Merchant
```bash
cbt -project ${GOOGLE_CLOUD_PROJECT} -instance ${GOOGLE_BIGTABLE_INSTANCE} sql "SELECT * FROM orders_daily_metrics_mv WHERE merchant_id = 'm_4'"
```

Lifetime Merchant Rollup
```bash
cbt -project ${GOOGLE_CLOUD_PROJECT} -instance ${GOOGLE_BIGTABLE_INSTANCE} sql "SELECT merchant_id, SUM(total_amount) AS lifetime_volume, SUM(order_count) AS lifetime_orders FROM orders_daily_metrics_mv WHERE merchant_id = 'm_4' GROUP BY merchant_id"
```

Fetch Recent Raw Transactions
```bash
cbt -project ${GOOGLE_CLOUD_PROJECT} -instance ${GOOGLE_BIGTABLE_INSTANCE} sql "SELECT CAST(_key AS STRING) AS row_key, CAST(core['amount'] AS STRING) AS amount, CAST(core['status'] AS STRING) AS status FROM orders WHERE CAST(_key AS STRING) LIKE 'm_4#%' LIMIT 100"
```


Date-Bounded Aggregations
```bash
cbt -project ${GOOGLE_CLOUD_PROJECT} -instance ${GOOGLE_BIGTABLE_INSTANCE} sql "SELECT merchant_id, SUM(total_amount) AS window_volume FROM orders_daily_metrics_mv WHERE merchant_id = 'm_4' AND day >= '2026-06-01 00:00:00' AND day <= '2026-07-01 00:00:00' GROUP BY merchant_id"
```


Date-Bounded Top 10 lists
```bash
cbt -project ${GOOGLE_CLOUD_PROJECT} -instance ${GOOGLE_BIGTABLE_INSTANCE} \
  sql "SELECT 
         merchant_id, 
         SUM(total_amount) AS window_volume 
       FROM orders_daily_metrics_mv 
       WHERE day >= '2026-07-01 00:00:00' AND day < '2026-08-01 00:00:00' 
       GROUP BY merchant_id 
       ORDER BY window_volume DESC 
       LIMIT 10"
```
