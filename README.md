This spins up a big VM, a Bigtable instance and has a built in benchmarker

## Setup Terraform

### Create a variable file
```bash
cp ./tfvars.example test.tfvars
```

### Spin up Terraform
```bash
terraform init
terraform apply -var-file=test.tfvars
```

## On the VM 

### See what tables are available

```bash
cbt -project ${GOOGLE_CLOUD_PROJECT} -instance ${GOOGLE_BIGTABLE_INSANCE} ls
```

```bash
git clone https://github.com/maguec/BTPerfTest
cd BTPerfTest
```

### Run the benchmark
```bash
go run benchit.go --project ${GOOGLE_CLOUD_PROJECT} --instance ${GOOGLE_BIGTABLE_INSTANCE} -z 100 -r 5000 -w 100000 -t run_40_extra -f cf1 -e 40
```

## Teardown Terraform

### Destroy Terraform
```bash
terraform destroy -var-file=test.tfvars
```
