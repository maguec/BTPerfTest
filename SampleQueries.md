### Fetch Daily Metrics for a Specific Merchant
```sql
cbt -project ${GOOGLE_CLOUD_PROJECT} -instance ${GOOGLE_BIGTABLE_INSTANCE} \
  sql "SELECT * FROM orders_daily_metrics_mv WHERE merchant_id = 'm_4' ORDER BY day DESC"
```

### Lifetime Merchant Rollup
```sql
cbt -project ${GOOGLE_CLOUD_PROJECT} -instance ${GOOGLE_BIGTABLE_INSTANCE} \
  sql "SELECT merchant_id, SUM(total_amount) AS lifetime_volume, SUM(order_count) AS lifetime_orders FROM orders_daily_metrics_mv WHERE merchant_id = 'm_4' GROUP BY merchant_id"
```

### Date-Bounded Aggregations
```sql
cbt -project ${GOOGLE_CLOUD_PROJECT} -instance ${GOOGLE_BIGTABLE_INSTANCE} \
  sql "SELECT merchant_id, SUM(total_amount) AS window_volume FROM orders_daily_metrics_mv WHERE merchant_id = 'm_4' AND day >= '2026-07-01 00:00:00' GROUP BY merchant_id"
```


### Top 10 by merchant
```sql
cbt -project ${GOOGLE_CLOUD_PROJECT} -instance ${GOOGLE_BIGTABLE_INSTANCE}   sql "SELECT
         merchant_id,
         SUM(total_amount) AS window_volume
       FROM orders_daily_metrics_mv
       WHERE day >= '2026-07-01 00:00:00' AND day < '2026-08-01 00:00:00'
       GROUP BY merchant_id
       ORDER BY window_volume DESC
       LIMIT 10"
```
