.PHONY: build clean build-tools build-main list-tables-tool prune-chaindata-tool compact-db-tool

build: build-tools build-main

build-main:
	go build -o prune-tool main.go

build-tools: list-tables-tool prune-chaindata-tool compact-db-tool

list-tables-tool:
	cd cmd/list-tables && go build -o ../../list-tables-tool .

prune-chaindata-tool:
	cd cmd/prune-chaindata && go build -o ../../prune-chaindata-tool .

compact-db-tool:
	cd cmd/compact-db && go build -o ../../compact-db-tool .

clean:
	rm -f prune-tool list-tables-tool prune-chaindata-tool compact-db-tool