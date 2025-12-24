BINARY_NAME=bootstrap

export GOOS=linux
export GOARCH=amd64
export CGO_ENABLED=0
export AWS_REGION=ap-south-1
export AWS_PROFILE=AWS_Personal

.DEFAULT_GOAL := deploy
docker:
	docker build -t lambda-kafka-build .
deploy:
	IMAGE_ID=$$(docker build -q --target build .) ; \
    docker create --name temp-container $$IMAGE_ID ; \
    docker cp temp-container:/app/bootstrap ./bootstrap ; \
    docker rm temp-container
	zip -r function.zip bootstrap
	aws lambda update-function-code --function-name "sqs-to-kafka-golang" --zip-file fileb://function.zip --region=${AWS_REGION} --profile=${AWS_PROFILE} | jq .    

run:
	go run ${BINARY_NAME}.go