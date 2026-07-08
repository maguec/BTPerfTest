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

```bash
go run seedorders.go --project ${GOOGLE_CLOUD_PROJECT} --instance ${GOOGLE_BIGTABLE_INSTANCE} --column-family=core --table orders
```
