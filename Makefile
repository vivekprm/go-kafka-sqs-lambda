BINARY_NAME=bootstrap

export AWS_REGION=us-east-1
export AWS_PROFILE=AWS_HOTMAIL

.DEFAULT_GOAL := release
docker:
	docker build -t lambda-kafka-build .
build:
	IMAGE_ID=$$(docker build -q --target build .) ; \
    docker create --name temp-container $$IMAGE_ID ; \
    docker cp temp-container:/app/bootstrap ./bootstrap ; \
    docker rm temp-container
zip:
	zip -r function.zip bootstrap
upload:
	aws lambda update-function-code --function-name "central-ec2-event-enrichment" --zip-file fileb://function.zip --region=${AWS_REGION} --profile=${AWS_PROFILE} | jq .
release: build zip upload
run:
	go run ${BINARY_NAME}.go

clean:
	rm -f function.zip bootstrap