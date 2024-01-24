package aws

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"packer-plugin-kubevirt/builder/common/utils"
)

func GetS3Uploader() (*s3manager.Uploader, error) {
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(utils.GetEnv("AWS_REGION", "us-gov-east-1"))},
	)
	if err != nil {
		return nil, err
	}
	return s3manager.NewUploader(sess), nil
}
