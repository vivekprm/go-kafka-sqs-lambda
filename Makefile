BINARY_NAME := bootstrap

export AWS_REGION=us-east-1
AWS_SOURCE_PROFILE=AWS_Personal
export AWS_PROFILE=AWS_HOTMAIL
export AWS_ACCOUNT_ID=665096241598
ROLE_ARN := "arn:aws:iam::$(AWS_ACCOUNT_ID):role/redundancy-data-poc-lambda-role"
export LAMBDA_FUNCTION_NAME=redundancy-data-poc
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
		--bucket $(LAMBDA_FUNCTION_NAME)-lambda \
		--region $(AWS_REGION)
	aws s3 cp function.zip s3://$(LAMBDA_FUNCTION_NAME)-lambda/function.zip --region=${AWS_REGION} --profile=${AWS_PROFILE}

createlambda:
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
    	--rule to-$(LAMBDA_FUNCTION_NAME) \
    	--targets '{"Id": "1", "Arn": "arn:aws:lambda:${AWS_REGION}:${AWS_ACCOUNT_ID}:function:${LAMBDA_FUNCTION_NAME}"}' \
		--profile ${AWS_PROFILE} \
		--region ${AWS_REGION}
	aws lambda add-permission \
		--function-name $(LAMBDA_FUNCTION_NAME) \
		--statement-id "RedundancyDataEventPermission" \
		--action "lambda:InvokeFunction" \
		--principal events.amazonaws.com \
		--source-arn arn:aws:events:${AWS_REGION}:${AWS_ACCOUNT_ID}:rule/to-$(LAMBDA_FUNCTION_NAME) \
		--profile ${AWS_PROFILE} \
		--region ${AWS_REGION}

trustpolicy:
	AWS_PAGER="" aws iam create-role \
		--role-name CentralAccountAccessRole \
		--profile ${AWS_PROFILE} \
		--assume-role-policy-document file://central-acc-role.json
	
	AWS_PAGER="" aws iam create-policy \
		--policy-name CentralAccountAccessPolicy \
		--profile ${AWS_PROFILE} \
		--policy-document file://central-acc-policy.json

	aws iam attach-role-policy \
		--role-name CentralAccountAccessRole \
		--policy-arn arn:aws:iam::${AWS_ACCOUNT_ID}:policy/CentralAccountAccessPolicy \
		--profile ${AWS_PROFILE}
	
	AWS_PAGER="" aws iam create-role \
		--role-name SourceLambdaExecutionRole
		--profile ${AWS_SOURCE_PROFILE} \
		--assume-role-policy-document file://source-acc-lambda-exec-role.json	
	
	AWS_PAGER="" aws iam create-policy \
		--policy-name SourceLambdaExecutionPolicy \
		--profile ${AWS_SOURCE_PROFILE} \
		--policy-document file://source-acc-lambda-exec-policy.json

	aws iam attach-role-policy \
		--role-name SourceLambdaExecutionRole \
		--policy-arn arn:aws:iam::${AWS_ACCOUNT_ID}:policy/SourceLambdaExecutionPolicy \
		--profile ${AWS_SOURCE_PROFILE}

poc: build zip upload-poc

poccleanup:
	aws s3 rm s3://$(LAMBDA_FUNCTION_NAME)-lambda/function.zip --region=${AWS_REGION} --profile=${AWS_PROFILE}
	aws s3 rb s3://$(LAMBDA_FUNCTION_NAME)-lambda --region=${AWS_REGION} --profile=${AWS_PROFILE}

run:
	go run ${BINARY_NAME}.go