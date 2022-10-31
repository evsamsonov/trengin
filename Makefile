lint:
	go mod download
	docker run --rm \
		-v ${GOPATH}/pkg/mod:/go/pkg/mod \
 		-v ${PWD}:/app \
 		-w /app \
	    golangci/golangci-lint:v1.49.0 \
	    golangci-lint run -v --modules-download-mode=readonly

test:
	GOARCH=amd64 go test ./...