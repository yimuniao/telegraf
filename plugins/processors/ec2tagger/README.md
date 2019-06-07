# EC2 Tagger Processor Plugin

Tags metrics with EC2 Metadata and EC2 Instance Tags.


## Amazon Authentication

This plugin uses a credential chain for Authentication with the EC2
API endpoint. In the following order the plugin will attempt to authenticate.
1. Assumed credentials via STS if `role_arn` attribute is specified (source credentials are evaluated from subsequent rules)
2. Explicit credentials from `access_key`, `secret_key`, and `token` attributes
3. Shared profile from `profile` attribute
4. [Environment Variables](https://github.com/aws/aws-sdk-go/wiki/configuring-sdk#environment-variables)
5. [Shared Credentials](https://github.com/aws/aws-sdk-go/wiki/configuring-sdk#shared-credentials-file)
6. [EC2 Instance Profile](http://docs.aws.amazon.com/AWSEC2/latest/UserGuide/iam-roles-for-amazon-ec2.html)

The IAM User or Role making the calls must have permissions to call the EC2 DescribeTags API.

### Configuration:

```toml
# Configuration for adding EC2 Metadata and Instance Tags to metrics.
[[processors.ec2tagger]]
  ##
  ## Frequency for the plugin to refresh the EC2 Instance Tags and values associated with this Instance.
  ## If not configured or zero, the default refresh interval of 3min will be used.
  ## If a negative value is given, the plugin does not refresh (uses the first tags and values retrieved, until restart).
  ## NOTE: due to how AutoScalingGroupName tag is setup by EC2, the AutoScalingGroupName tag might not be available at the start, and if no refresh is configured, the AutoScalingGroupName may never be available.
  ## Defaults to 180s (3 minutes)
  # refresh_interval = "180s"
  ##
  ## Add tags for EC2 Metadata fields.
  ## Supported fields are: "InstanceId", "ImageId" (aka AMI), "InstanceType"
  ## If the configuration is not provided or it has an empty list, no EC2 Metadata tags are applied.
  # ec2_metadata_tags = ["InstanceId", "ImageId", "InstanceType"]
  ##
  ## Add tags retrieved from the EC2 Instance Tags associated with this instance.
  ## If this configuration is not provided, or has an empty list, no EC2 Instance Tags are applied.
  ## If this configuration contains one entry and its value is "*", then ALL EC2 Instance Tags for the instance are applied.
  ## Note: This plugin renames the "aws:autoscaling:groupName" EC2 Instance Tag key to be spelled "AutoScalingGroupName".
  ## This aligns it with the AutoScaling dimension-name seen in AWS CloudWatch.
  # ec2_instance_tag_keys = ["aws:autoscaling:groupName", "Name"]
  ##
  ## Amazon Credentials
  ## Credentials are loaded in the following order
  ## 1) Assumed credentials via STS if role_arn is specified
  ## 2) explicit credentials from 'access_key' and 'secret_key'
  ## 3) shared profile from 'profile'
  ## 4) environment variables
  ## 5) shared credentials file
  ## 6) EC2 Instance Profile
  # access_key = ""
  # secret_key = ""
  # token = ""
  # role_arn = ""
  # profile = ""
  # shared_credential_file = ""
```

### Filters:

Processor plugins support the standard tag filter settings just like everything else.

