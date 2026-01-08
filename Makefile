BINARY_NAME := bootstrap

export AWS_REGION=ap-south-1
export AWS_PROFILE=AWS_Personal
export AWS_ACCOUNT_ID=643716337869
ROLE_ARN := "arn:aws:iam::$(AWS_ACCOUNT_ID):role/redundancy-data-poc-lambda-role"
LAMBDA_FUNCTION_NAME := redundancy-data-poc
DATA_BUCKET_KEY := data.json

.DEFAULT_GOAL := deploy

build:
	IMAGE_ID=$$(docker build -q --target build .) ; \
	echo $$IMAGE_ID ; \
    docker create --name temp-container $$IMAGE_ID ; \
    docker cp temp-container:/app/bootstrap ./bootstrap ; \
    docker rm temp-container

zip:
	zip -r function.zip bootstrap

deploy: build zip
	aws lambda update-function-code --function-name $(LAMBDA_FUNCTION_NAME) --zip-file fileb://function.zip --region=${AWS_REGION} --profile=${AWS_PROFILE} | jq .    

upload-poc: 
	aws s3api create-bucket \
		--profile ${AWS_PROFILE} \
		--bucket $(LAMBDA_FUNCTION_NAME) \
		--region $(AWS_REGION) \
		--create-bucket-configuration LocationConstraint=$(AWS_REGION)
	aws s3 cp function.zip s3://$(LAMBDA_FUNCTION_NAME)/function.zip --region=${AWS_REGION} --profile=${AWS_PROFILE}
	aws s3 cp data.json s3://$(LAMBDA_FUNCTION_NAME)/data.json --region=${AWS_REGION} --profile=${AWS_PROFILE}

poc: upload-poc
	AWS_PAGER="" aws lambda create-function \
		--function-name $(LAMBDA_FUNCTION_NAME) \
		--runtime "provided.al2023" \
		--role $(ROLE_ARN) \
		--handler bootstrap \
		--architecture arm64 \
		--zip-file fileb://function.zip \
		--region=${AWS_REGION} \
		--profile=${AWS_PROFILE} \
		--environment '{"Variables":{"BUCKET_NAME":"${LAMBDA_FUNCTION_NAME}","BUCKET_KEY":"${DATA_BUCKET_KEY}"}}'

createrule:
	aws events put-rule \
		--name "to-$(LAMBDA_FUNCTION_NAME)" \
		--event-pattern '{"source": ["aws.ec2"], "detail-type": ["EC2 Instance State-change Notification"]}' \
		--region=${AWS_REGION} \
		--profile=${AWS_PROFILE}

	aws events put-targets \
    	--rule my-scheduled-rule \
    	--targets '{"Id": "1", "Arn": "arn:aws:lambda:${AWS_REGION}:${AWS_ACCOUNT_ID}:function:${LAMBDA_FUNCTION_NAME}"}'


poc_clean:
	aws s3 rm s3://$(LAMBDA_FUNCTION_NAME)/function.zip --region=${AWS_REGION} --profile=${AWS_PROFILE}
	aws s3 rm s3://$(LAMBDA_FUNCTION_NAME)/data.json --region=${AWS_REGION} --profile=${AWS_PROFILE}
	aws s3 rb s3://$(LAMBDA_FUNCTION_NAME) --region=${AWS_REGION} --profile=${AWS_PROFILE}
	AWS_PAGER="" aws lambda delete-function \
		--function-name $(LAMBDA_FUNCTION_NAME) \
		--region=${AWS_REGION} \
		--profile=${AWS_PROFILE}

run:
	go run ${BINARY_NAME}.go