package ec2tagger

import (
	"fmt"
	"hash/fnv"
	"log"
	"os"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal"
	internalaws "github.com/influxdata/telegraf/internal/config/aws"
	"github.com/influxdata/telegraf/plugins/processors"
)

// Reminder, keep this in sync with the plugin's README.md
const sampleConfig = `
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
`

const (
	ec2InstanceTagKeyASG   = "aws:autoscaling:groupName"
	cwDimensionASG         = "AutoScalingGroupName"
	mdKeyInstanceId        = "InstanceId"
	mdKeyImageId           = "ImageId"
	mdKeyInstaneType       = "InstanceType"
	defaultRefreshInterval = 180 * time.Second
)

// Interfaces for testing purposes
type ec2metadataAPI interface {
	GetInstanceIdentityDocument() (ec2metadata.EC2InstanceIdentityDocument, error)
	Available() bool
}

type ec2APIProvider interface {
	newEC2(p client.ConfigProvider, cfgs ...*aws.Config) ec2iface.EC2API
	newEC2Metadata(p client.ConfigProvider, cfgs ...*aws.Config) ec2metadataAPI
}

type awsEC2APIProvider struct{}

func (a awsEC2APIProvider) newEC2(p client.ConfigProvider, cfgs ...*aws.Config) ec2iface.EC2API {
	return ec2.New(p, cfgs...)
}
func (a awsEC2APIProvider) newEC2Metadata(p client.ConfigProvider, cfgs ...*aws.Config) ec2metadataAPI {
	return ec2metadata.New(p, cfgs...)
}

type Tagger struct {
	RefreshInterval    internal.Duration `toml:"refresh_interval"`
	EC2MetadataTags    []string          `toml:"ec2_metadata_tags"`
	EC2InstanceTagKeys []string          `toml:"ec2_instance_tag_keys"`
	// unlike other AWS plugins, this one determines the region from ec2 metadata not user configuration
	AccessKey string `toml:"access_key"`
	SecretKey string `toml:"secret_key"`
	RoleARN   string `toml:"role_arn"`
	Profile   string `toml:"profile"`
	Filename  string `toml:"shared_credential_file"`
	Token     string `toml:"token"`

	ec2APIProvider ec2APIProvider

	// ec2 metadata
	instanceId   string
	instanceType string
	imageId      string
	region       string

	ec2TagsLock  sync.RWMutex
	ec2TagsCache map[string]string
	done         chan struct{}
}

// init adds this plugin to the framework's "processors" registry
func init() {
	processors.Add("ec2tagger", func() telegraf.Processor {
		return &Tagger{ec2APIProvider: awsEC2APIProvider{}}
	})
}

func (t *Tagger) SampleConfig() string {
	return sampleConfig
}

func (t *Tagger) Description() string {
	return "Configuration for adding EC2 Metadata and Instance Tags to metrics."
}

func (t *Tagger) Start() error {
	if err := t.getEC2Metadata(); err != nil {
		return fmt.Errorf("E! ec2tagger: failed to retrieve ec2 metadata: %+v", err)
	}
	if t.isGettingEC2Tags() {
		t.done = make(chan struct{})
		if t.RefreshInterval.Duration == 0 {
			t.RefreshInterval.Duration = defaultRefreshInterval
		}

		fetchEC2Tags := t.getEC2TagsFetchFunc()
		tags, err := fetchEC2Tags()
		if err != nil {
			return fmt.Errorf("E! ec2tagger: Failed to start ec2tagger: %+v", err)
		}
		t.ec2TagsCache = tags

		if t.isRefreshingEC2Tags() {
			go t.refreshTags(fetchEC2Tags)
		}
	}
	return nil
}

func (t *Tagger) Stop() {
	if t.done != nil {
		close(t.done)
	}
}

// Apply adds the configured EC2 Metadata and Instance Tags to metrics.
// This is called serially for ALL metrics (that pass the plugin's tag filters) so keep it fast.
func (t *Tagger) Apply(in ...telegraf.Metric) []telegraf.Metric {
	t.ec2TagsLock.RLock()
	tags := t.ec2TagsCache
	t.ec2TagsLock.RUnlock()
	for _, metric := range in {
		for k, v := range tags {
			metric.AddTag(k, v)
		}
		for _, tag := range t.EC2MetadataTags {
			switch tag {
			case mdKeyInstanceId:
				metric.AddTag(mdKeyInstanceId, t.instanceId)
			case mdKeyImageId:
				metric.AddTag(mdKeyImageId, t.imageId)
			case mdKeyInstaneType:
				metric.AddTag(mdKeyInstaneType, t.instanceType)
			default:
				log.Fatalf("E! ec2tagger: Unsupported EC2 Metadata key: %s\n", tag)
			}
		}
	}
	return in
}

func (t *Tagger) refreshTags(refreshFunc func() (map[string]string, error)) {
	time.Sleep(hostJitter(t.RefreshInterval.Duration))
	ticker := time.NewTicker(t.RefreshInterval.Duration)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			newTags, err := refreshFunc()
			if err != nil {
				log.Printf("I! ec2tagger: Error refreshing EC2 tags, keeping old values : +%v\n", err.Error())
				continue
			}
			t.ec2TagsLock.Lock()
			t.ec2TagsCache = newTags
			t.ec2TagsLock.Unlock()
		case <-t.done:
			return
		}
	}
}

func (t *Tagger) getEC2TagsFetchFunc() func() (map[string]string, error) {
	ec2CredentialConfig := &internalaws.CredentialConfig{
		Region:    t.region,
		AccessKey: t.AccessKey,
		SecretKey: t.SecretKey,
		RoleARN:   t.RoleARN,
		Profile:   t.Profile,
		Filename:  t.Filename,
		Token:     t.Token,
	}
	ec2Client := t.ec2APIProvider.newEC2(ec2CredentialConfig.Credentials())

	tagFilters := []*ec2.Filter{
		{
			Name:   aws.String("resource-type"),
			Values: aws.StringSlice([]string{"instance"}),
		},
		{
			Name:   aws.String("resource-id"),
			Values: aws.StringSlice([]string{t.instanceId}),
		},
	}

	if !t.isGettingAllEC2Tags() {
		// if the customer said 'AutoScalingGroupName' (the CW dimension), do what they mean not what they said
		// and filter for the EC2 tag name called 'aws:autoscaling:groupName'
		for i, key := range t.EC2InstanceTagKeys {
			if key == cwDimensionASG {
				t.EC2InstanceTagKeys[i] = ec2InstanceTagKeyASG
			}
		}

		tagFilters = append(tagFilters, &ec2.Filter{
			Name:   aws.String("key"),
			Values: aws.StringSlice(t.EC2InstanceTagKeys),
		})
	}

	return func() (map[string]string, error) {

		tags := make(map[string]string)

		input := &ec2.DescribeTagsInput{
			Filters: tagFilters,
		}

		for {
			result, err := ec2Client.DescribeTags(input)
			if err != nil {
				return nil, err
			}
			for _, tag := range result.Tags {
				key := *tag.Key
				if key == ec2InstanceTagKeyASG {
					// rename to match CW dimension as applied by AutoScaling service, not the EC2 tag
					key = cwDimensionASG
				}
				tags[key] = *tag.Value
			}
			if result.NextToken == nil {
				break
			}
			input.SetNextToken(*result.NextToken)
		}
		return tags, nil
	}
}

func (t *Tagger) getEC2Metadata() error {
	md := t.ec2APIProvider.newEC2Metadata((&internalaws.CredentialConfig{}).Credentials())
	if !md.Available() {
		return fmt.Errorf("E! ec2tagger: Unable to retrieve InstanceId. This plugin must only be used on an EC2 instance\n")
	}
	doc, err := md.GetInstanceIdentityDocument()
	if err != nil {
		return fmt.Errorf("E! ec2tagger: Unable to retrieve InstanceId : %+v\n", err.Error())
	}
	t.instanceId = doc.InstanceID
	t.instanceType = doc.InstanceType
	t.imageId = doc.ImageID
	t.region = doc.Region
	return nil
}

func (t Tagger) isGettingEC2Tags() bool {
	return len(t.EC2InstanceTagKeys) > 0
}

func (t Tagger) isGettingAllEC2Tags() bool {
	return len(t.EC2InstanceTagKeys) == 1 && t.EC2InstanceTagKeys[0] == "*"
}

func (t Tagger) isRefreshingEC2Tags() bool {
	return t.RefreshInterval.Duration > 0
}

func hostJitter(max time.Duration) time.Duration {
	hostname, _ := os.Hostname()
	hash := fnv.New64()
	hash.Write([]byte(hostname))
	// Right shift the uint64 hash by one to make sure the jitter duration is always positive
	return time.Duration(int64(hash.Sum64()>>1)) % max
}
