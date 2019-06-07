package ec2tagger

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/plugins/processors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestHostJitter(t *testing.T) {
	returnJitter := hostJitter(time.Minute)
	assert.True(t, returnJitter >= 0)
	assert.True(t, returnJitter < time.Minute)
}

type MockEC2MetadataAPI struct {
	mock.Mock
}

func (m *MockEC2MetadataAPI) GetInstanceIdentityDocument() (ec2metadata.EC2InstanceIdentityDocument, error) {
	args := m.Called()
	return args.Get(0).(ec2metadata.EC2InstanceIdentityDocument), args.Error(1)
}

func (m *MockEC2MetadataAPI) Available() bool {
	args := m.Called()
	return args.Bool(0)
}

type MockEC2API struct {
	mock.Mock
	ec2iface.EC2API
}

func (m *MockEC2API) DescribeTags(input *ec2.DescribeTagsInput) (*ec2.DescribeTagsOutput, error) {
	args := m.Called(input)
	return args.Get(0).(*ec2.DescribeTagsOutput), args.Error(1)
}

type MockEC2APIProvider struct {
	mock.Mock
}

func (m *MockEC2APIProvider) newEC2(p client.ConfigProvider, cfgs ...*aws.Config) ec2iface.EC2API {
	args := m.Called(p, cfgs)
	return args.Get(0).(ec2iface.EC2API)
}
func (m *MockEC2APIProvider) newEC2Metadata(p client.ConfigProvider, cfgs ...*aws.Config) ec2metadataAPI {
	args := m.Called(p, cfgs)
	return args.Get(0).(ec2metadataAPI)
}

var normalTests = []struct {
	name     string
	metadata ec2metadata.EC2InstanceIdentityDocument
	tags     ec2.DescribeTagsOutput
	tagger   *Tagger
	input    []telegraf.Metric
	expected []telegraf.Metric
}{
	{
		name: "All metadata and all tags",
		metadata: ec2metadata.EC2InstanceIdentityDocument{
			InstanceID:   "TESTINSTANCEID",
			InstanceType: "TESTINSTANCETYPE",
			ImageID:      "TESTIMAGEID",
			Region:       "TESTREGION",
		},
		tags: ec2.DescribeTagsOutput{
			Tags: []*ec2.TagDescription{
				&ec2.TagDescription{Key: aws.String("TESTTAG"), Value: aws.String("TESTTAGVALUE")},
				&ec2.TagDescription{Key: aws.String("TESTTAG2"), Value: aws.String("TESTTAG2VALUE")},
				&ec2.TagDescription{Key: aws.String("TESTTAG3"), Value: aws.String("TESTTAG3VALUE")},
			},
		},
		tagger: &Tagger{
			RefreshInterval:    internal.Duration{time.Second},
			EC2MetadataTags:    []string{mdKeyInstanceId, mdKeyInstaneType, mdKeyImageId},
			EC2InstanceTagKeys: []string{"*"},
			AccessKey:          "TESTACCESKEY",
			SecretKey:          "TESTSECRETKEY",
			RoleARN:            "TESTROLEARN",
			Profile:            "TESTPROFILE",
			Filename:           "TESTFILENAME",
			Token:              "TESTTOKEN",
		},
		input: []telegraf.Metric{newMetric(nil)},
		expected: []telegraf.Metric{newMetric(map[string]string{
			"TESTTAG":        "TESTTAGVALUE",
			"TESTTAG2":       "TESTTAG2VALUE",
			"TESTTAG3":       "TESTTAG3VALUE",
			mdKeyInstanceId:  "TESTINSTANCEID",
			mdKeyInstaneType: "TESTINSTANCETYPE",
			mdKeyImageId:     "TESTIMAGEID",
		})},
	},
	{
		name: "AutoScalingGroupName config translated",
		metadata: ec2metadata.EC2InstanceIdentityDocument{
			Region: "TESTREGION",
		},
		tags: ec2.DescribeTagsOutput{
			Tags: []*ec2.TagDescription{
				&ec2.TagDescription{Key: aws.String("aws:autoscaling:groupName"), Value: aws.String("TESTASGNAME")},
			},
		},
		tagger: &Tagger{
			RefreshInterval:    internal.Duration{time.Second},
			EC2MetadataTags:    []string{},
			EC2InstanceTagKeys: []string{"AutoScalingGroupName"},
		},
		input: []telegraf.Metric{newMetric(nil)},
		expected: []telegraf.Metric{newMetric(map[string]string{
			"AutoScalingGroupName": "TESTASGNAME",
		})},
	},
	{
		name: "AutoScalingGroupName key translated",
		metadata: ec2metadata.EC2InstanceIdentityDocument{
			Region: "TESTREGION",
		},
		tags: ec2.DescribeTagsOutput{
			Tags: []*ec2.TagDescription{
				&ec2.TagDescription{Key: aws.String("aws:autoscaling:groupName"), Value: aws.String("TESTASGNAME")},
			},
		},
		tagger: &Tagger{
			RefreshInterval:    internal.Duration{time.Second},
			EC2MetadataTags:    []string{},
			EC2InstanceTagKeys: []string{"aws:autoscaling:groupName"},
		},
		input: []telegraf.Metric{newMetric(nil)},
		expected: []telegraf.Metric{newMetric(map[string]string{
			"AutoScalingGroupName": "TESTASGNAME",
		})},
	},
	{
		name: "no metadata and all tags",
		metadata: ec2metadata.EC2InstanceIdentityDocument{
			InstanceID:   "TESTINSTANCEID",
			InstanceType: "TESTINSTANCETYPE",
			ImageID:      "TESTIMAGEID",
			Region:       "TESTREGION",
		},
		tags: ec2.DescribeTagsOutput{
			Tags: []*ec2.TagDescription{
				&ec2.TagDescription{Key: aws.String("TESTTAG"), Value: aws.String("TESTTAGVALUE")},
				&ec2.TagDescription{Key: aws.String("TESTTAG2"), Value: aws.String("TESTTAG2VALUE")},
				&ec2.TagDescription{Key: aws.String("TESTTAG3"), Value: aws.String("TESTTAG3VALUE")},
			},
		},
		tagger: &Tagger{
			RefreshInterval:    internal.Duration{time.Second},
			EC2MetadataTags:    []string{},
			EC2InstanceTagKeys: []string{"*"},
		},
		input: []telegraf.Metric{newMetric(nil)},
		expected: []telegraf.Metric{newMetric(map[string]string{
			"TESTTAG":  "TESTTAGVALUE",
			"TESTTAG2": "TESTTAG2VALUE",
			"TESTTAG3": "TESTTAG3VALUE",
		})},
	},
	{
		name: "no metadata and some tags",
		metadata: ec2metadata.EC2InstanceIdentityDocument{
			InstanceID:   "TESTINSTANCEID",
			InstanceType: "TESTINSTANCETYPE",
			ImageID:      "TESTIMAGEID",
			Region:       "TESTREGION",
		},
		tags: ec2.DescribeTagsOutput{
			Tags: []*ec2.TagDescription{
				&ec2.TagDescription{Key: aws.String("TESTTAG"), Value: aws.String("TESTTAGVALUE")},
				&ec2.TagDescription{Key: aws.String("TESTTAG3"), Value: aws.String("TESTTAG3VALUE")},
			},
		},
		tagger: &Tagger{
			RefreshInterval:    internal.Duration{time.Second},
			EC2MetadataTags:    []string{},
			EC2InstanceTagKeys: []string{"TESTTAG", "TESTTAG3"},
		},
		input: []telegraf.Metric{newMetric(nil)},
		expected: []telegraf.Metric{newMetric(map[string]string{
			"TESTTAG":  "TESTTAGVALUE",
			"TESTTAG3": "TESTTAG3VALUE",
		})},
	},
}

func TestTaggerNormal(t *testing.T) {
	for _, tt := range normalTests {
		t.Run(tt.name, func(t *testing.T) {
			em := new(MockEC2MetadataAPI)
			em.On("Available").Return(true)
			em.On("GetInstanceIdentityDocument").Return(tt.metadata, nil)

			e := new(MockEC2API)
			e.On("DescribeTags", mock.Anything).Return(&tt.tags, nil)

			p := new(MockEC2APIProvider)
			p.On("newEC2", mock.MatchedBy(
				func(c client.ConfigProvider) bool {
					s := c.(*session.Session)
					if *s.Config.Region != tt.metadata.Region {
						return false
					}
					return true
				}),
				mock.Anything,
			).Return(e)
			p.On("newEC2Metadata", mock.Anything, mock.Anything).Return(em)

			tt.tagger.ec2APIProvider = p

			err := tt.tagger.Start()
			assert.Nil(t, err)

			output := tt.tagger.Apply(tt.input...)
			assert.Equal(t, len(output), len(tt.expected))
			for i := 0; i < len(output); i++ {
				o, e := output[i], tt.expected[i]
				assert.True(t, reflect.DeepEqual(o.Tags(), e.Tags()))
			}

			tt.tagger.Stop()

			em.AssertExpectations(t)
			e.AssertExpectations(t)
			p.AssertExpectations(t)
		})
	}
}

func TestTaggerEC2APIRequestFilter(t *testing.T) {
	t.Run("tag keys filter added correctly", func(t *testing.T) {
		em := new(MockEC2MetadataAPI)
		em.On("Available").Return(true)
		em.On("GetInstanceIdentityDocument").Return(ec2metadata.EC2InstanceIdentityDocument{
			InstanceID: "TESTINSTANCEID",
			Region:     "TESTREGION",
		}, nil)

		e := new(MockEC2API)
		e.On("DescribeTags", mock.MatchedBy(func(input *ec2.DescribeTagsInput) bool {
			for _, f := range input.Filters {
				if *f.Name == "key" {
					if len(f.Values) != 2 || *f.Values[0] != "TESTTAG" || *f.Values[1] != "TESTTAG3" {
						return false
					}
				}
				if *f.Name == "resource-type" {
					if len(f.Values) != 1 || *f.Values[0] != "instance" {
						return false
					}
				}
				if *f.Name == "resource-id" {
					if len(f.Values) != 1 || *f.Values[0] != "TESTINSTANCEID" {
						return false
					}
				}
			}
			return true
		})).Return(&ec2.DescribeTagsOutput{Tags: []*ec2.TagDescription{}}, nil)

		p := new(MockEC2APIProvider)
		p.On("newEC2", mock.Anything, mock.Anything).Return(e)
		p.On("newEC2Metadata", mock.Anything, mock.Anything).Return(em)

		tagger := &Tagger{
			EC2InstanceTagKeys: []string{"TESTTAG", "TESTTAG3"},
			ec2APIProvider:     p,
		}

		err := tagger.Start()
		assert.Nil(t, err)

		tagger.Stop()

		em.AssertExpectations(t)
		e.AssertExpectations(t)
		p.AssertExpectations(t)
	})
	t.Run("no tag key filter when EC2InstanceTagKeys is '*'", func(t *testing.T) {
		em := new(MockEC2MetadataAPI)
		em.On("Available").Return(true)
		em.On("GetInstanceIdentityDocument").Return(ec2metadata.EC2InstanceIdentityDocument{}, nil)

		e := new(MockEC2API)
		e.On("DescribeTags", mock.MatchedBy(func(input *ec2.DescribeTagsInput) bool {
			for _, f := range input.Filters {
				if *f.Name == "key" {
					return false
				}
			}
			return true
		})).Return(&ec2.DescribeTagsOutput{Tags: []*ec2.TagDescription{}}, nil)

		p := new(MockEC2APIProvider)
		p.On("newEC2", mock.Anything, mock.Anything).Return(e)
		p.On("newEC2Metadata", mock.Anything, mock.Anything).Return(em)

		tagger := &Tagger{
			EC2InstanceTagKeys: []string{"*"},
			ec2APIProvider:     p,
		}

		err := tagger.Start()
		assert.Nil(t, err)

		tagger.Stop()

		em.AssertExpectations(t)
		e.AssertExpectations(t)
		p.AssertExpectations(t)
	})
}

func TestTaggerOnlyMetadataNoTag(t *testing.T) {
	t.Run("only metadata without tag should not create refresh goroutine", func(t *testing.T) {
		em := new(MockEC2MetadataAPI)
		em.On("Available").Return(true)
		em.On("GetInstanceIdentityDocument").Return(ec2metadata.EC2InstanceIdentityDocument{Region: "TESTREGION"}, nil)

		p := new(MockEC2APIProvider)
		p.On("newEC2Metadata", mock.Anything, mock.Anything).Return(em)

		tagger := &Tagger{
			EC2MetadataTags: []string{mdKeyInstanceId, mdKeyInstaneType, mdKeyImageId},
			ec2APIProvider:  p,
		}

		err := tagger.Start()
		assert.Nil(t, err)

		tagger.Stop()

		em.AssertExpectations(t)
		p.AssertExpectations(t)
		p.AssertNotCalled(t, "newEC2", mock.Anything, mock.Anything)
		assert.Nil(t, tagger.done)
	})
}

func TestTaggerRefreshInterval(t *testing.T) {
	t.Run("Default refresh interval should be used when not configured", func(t *testing.T) {
		em := new(MockEC2MetadataAPI)
		em.On("Available").Return(true)
		em.On("GetInstanceIdentityDocument").Return(ec2metadata.EC2InstanceIdentityDocument{Region: "TESTREGION"}, nil)

		e := new(MockEC2API)
		e.On("DescribeTags", mock.Anything).Return(&ec2.DescribeTagsOutput{Tags: []*ec2.TagDescription{}}, nil)

		p := new(MockEC2APIProvider)
		p.On("newEC2", mock.Anything, mock.Anything).Return(e)
		p.On("newEC2Metadata", mock.Anything, mock.Anything).Return(em)

		tagger := &Tagger{
			EC2InstanceTagKeys: []string{"*"},
			ec2APIProvider:     p,
		}

		err := tagger.Start()
		assert.Nil(t, err)
		tagger.Stop()
		assert.Equal(t, defaultRefreshInterval, tagger.RefreshInterval.Duration)
	})
	t.Run("RefreshInterval configured should not be modified", func(t *testing.T) {
		em := new(MockEC2MetadataAPI)
		em.On("Available").Return(true)
		em.On("GetInstanceIdentityDocument").Return(ec2metadata.EC2InstanceIdentityDocument{Region: "TESTREGION"}, nil)

		e := new(MockEC2API)
		e.On("DescribeTags", mock.Anything).Return(&ec2.DescribeTagsOutput{Tags: []*ec2.TagDescription{}}, nil)

		p := new(MockEC2APIProvider)
		p.On("newEC2", mock.Anything, mock.Anything).Return(e)
		p.On("newEC2Metadata", mock.Anything, mock.Anything).Return(em)
		tagger := &Tagger{
			EC2InstanceTagKeys: []string{"*"},
			RefreshInterval:    internal.Duration{5 * time.Second},
			ec2APIProvider:     p,
		}

		err := tagger.Start()
		assert.Nil(t, err)
		tagger.Stop()
		assert.Equal(t, 5*time.Second, tagger.RefreshInterval.Duration)
	})
	t.Run("negative RefreshInterval will not refresh tags", func(t *testing.T) {
		em := new(MockEC2MetadataAPI)
		em.On("Available").Return(true)
		em.On("GetInstanceIdentityDocument").Return(ec2metadata.EC2InstanceIdentityDocument{Region: "TESTREGION"}, nil)

		e := new(MockEC2API)
		e.On("DescribeTags", mock.Anything).Return(&ec2.DescribeTagsOutput{Tags: []*ec2.TagDescription{}}, nil)

		p := new(MockEC2APIProvider)
		p.On("newEC2", mock.Anything, mock.Anything).Return(e)
		p.On("newEC2Metadata", mock.Anything, mock.Anything).Return(em)
		tagger := &Tagger{
			EC2InstanceTagKeys: []string{"*"},
			RefreshInterval:    internal.Duration{-1 * time.Millisecond},
			ec2APIProvider:     p,
		}

		err := tagger.Start()
		assert.Nil(t, err)

		time.Sleep(100 * time.Millisecond)
		tagger.Stop()

		e.AssertNumberOfCalls(t, "DescribeTags", 1)
	})
}

func TestTaggerStartError(t *testing.T) {
	t.Run("instance metadata not available results in start error", func(t *testing.T) {
		em := new(MockEC2MetadataAPI)
		em.On("Available").Return(false)

		p := new(MockEC2APIProvider)
		p.On("newEC2Metadata", mock.Anything, mock.Anything).Return(em)

		tagger := &Tagger{
			EC2InstanceTagKeys: []string{"*"},
			ec2APIProvider:     p,
		}

		err := tagger.Start()
		assert.NotNil(t, err)
	})
	t.Run("error from getting instance metadata results in start error", func(t *testing.T) {
		em := new(MockEC2MetadataAPI)
		em.On("Available").Return(true)
		em.On("GetInstanceIdentityDocument").Return(ec2metadata.EC2InstanceIdentityDocument{}, errors.New("TESTERR"))

		p := new(MockEC2APIProvider)
		p.On("newEC2Metadata", mock.Anything, mock.Anything).Return(em)

		tagger := &Tagger{
			EC2InstanceTagKeys: []string{"*"},
			ec2APIProvider:     p,
		}

		err := tagger.Start()
		assert.NotNil(t, err)
	})
	t.Run("error from getting ec2 tags results in start error", func(t *testing.T) {
		em := new(MockEC2MetadataAPI)
		em.On("Available").Return(true)
		em.On("GetInstanceIdentityDocument").Return(ec2metadata.EC2InstanceIdentityDocument{}, nil)

		e := new(MockEC2API)
		var np *ec2.DescribeTagsOutput
		e.On("DescribeTags", mock.Anything).Return(np, errors.New("TESTERR"))

		p := new(MockEC2APIProvider)
		p.On("newEC2", mock.Anything, mock.Anything).Return(e)
		p.On("newEC2Metadata", mock.Anything, mock.Anything).Return(em)

		tagger := &Tagger{
			EC2InstanceTagKeys: []string{"*"},
			ec2APIProvider:     p,
		}

		err := tagger.Start()
		assert.NotNil(t, err)
	})
}

func TestTaggerTagsRefresh(t *testing.T) {
	t.Run("tag gets refreshed at correct interval", func(t *testing.T) {
		em := new(MockEC2MetadataAPI)
		em.On("Available").Return(true)
		em.On("GetInstanceIdentityDocument").Return(ec2metadata.EC2InstanceIdentityDocument{}, nil)

		i := 0
		e := new(MockEC2API)
		e.
			On("DescribeTags", mock.MatchedBy(func(v interface{}) bool {
				if i == 0 {
					i = 1
					return true
				}
				return false
			})).
			Return(&ec2.DescribeTagsOutput{Tags: []*ec2.TagDescription{
				&ec2.TagDescription{Key: aws.String("TESTTAG"), Value: aws.String("TESTTAGVALUE1")},
			}}, nil).
			On("DescribeTags", mock.MatchedBy(func(v interface{}) bool {
				if i == 1 {
					i = 2
					return true
				}
				return false
			})).
			Return(&ec2.DescribeTagsOutput{Tags: []*ec2.TagDescription{
				&ec2.TagDescription{Key: aws.String("TESTTAG"), Value: aws.String("TESTTAGVALUE2")},
			}}, nil).
			On("DescribeTags", mock.Anything).
			Return(&ec2.DescribeTagsOutput{Tags: []*ec2.TagDescription{
				&ec2.TagDescription{Key: aws.String("TESTTAG"), Value: aws.String("TESTTAGVALUE3")},
			}}, nil)

		p := new(MockEC2APIProvider)
		p.On("newEC2", mock.Anything, mock.Anything).Return(e)
		p.On("newEC2Metadata", mock.Anything, mock.Anything).Return(em)

		tagger := &Tagger{
			RefreshInterval:    internal.Duration{10 * time.Millisecond},
			EC2InstanceTagKeys: []string{"*"},
			ec2APIProvider:     p,
		}

		err := tagger.Start()
		assert.Nil(t, err)
		defer tagger.Stop()

		m1 := newMetric(nil)
		r1 := tagger.Apply(m1)[0]
		e1 := newMetric(map[string]string{"TESTTAG": "TESTTAGVALUE1"})
		assert.True(t, reflect.DeepEqual(r1.Tags(), e1.Tags()))

		time.Sleep(20 * time.Millisecond)

		m2a := newMetric(nil)
		r2a := tagger.Apply(m2a)[0]
		e2a := newMetric(map[string]string{"TESTTAG": "TESTTAGVALUE2"})
		assert.True(t, reflect.DeepEqual(r2a.Tags(), e2a.Tags()))

		m2b := newMetric(nil)
		r2b := tagger.Apply(m2b)[0]
		e2b := newMetric(map[string]string{"TESTTAG": "TESTTAGVALUE2"})
		assert.True(t, reflect.DeepEqual(r2b.Tags(), e2b.Tags()))

		time.Sleep(10 * time.Millisecond)

		m3 := newMetric(nil)
		r3 := tagger.Apply(m3)[0]
		e3 := newMetric(map[string]string{"TESTTAG": "TESTTAGVALUE3"})
		assert.True(t, reflect.DeepEqual(r3.Tags(), e3.Tags()))
	})
	t.Run("API error during refresh does not stop future refresh", func(t *testing.T) {
		em := new(MockEC2MetadataAPI)
		em.On("Available").Return(true)
		em.On("GetInstanceIdentityDocument").Return(ec2metadata.EC2InstanceIdentityDocument{}, nil)

		i := 0
		var np *ec2.DescribeTagsOutput
		e := new(MockEC2API)
		e.
			On("DescribeTags", mock.MatchedBy(func(v interface{}) bool {
				if i == 0 {
					i = 1
					return true
				}
				return false
			})).
			Return(&ec2.DescribeTagsOutput{Tags: []*ec2.TagDescription{
				&ec2.TagDescription{Key: aws.String("TESTTAG"), Value: aws.String("TESTTAGVALUE1")},
			}}, nil).
			On("DescribeTags", mock.MatchedBy(func(v interface{}) bool {
				if i == 1 {
					i = 2
					return true
				}
				return false
			})).
			Return(np, errors.New("TESTERR")).
			On("DescribeTags", mock.Anything).
			Return(&ec2.DescribeTagsOutput{Tags: []*ec2.TagDescription{
				&ec2.TagDescription{Key: aws.String("TESTTAG"), Value: aws.String("TESTTAGVALUE3")},
			}}, nil)

		p := new(MockEC2APIProvider)
		p.On("newEC2", mock.Anything, mock.Anything).Return(e)
		p.On("newEC2Metadata", mock.Anything, mock.Anything).Return(em)

		tagger := &Tagger{
			RefreshInterval:    internal.Duration{10 * time.Millisecond},
			EC2InstanceTagKeys: []string{"*"},
			ec2APIProvider:     p,
		}

		err := tagger.Start()
		assert.Nil(t, err)
		defer tagger.Stop()

		m1 := newMetric(nil)
		r1 := tagger.Apply(m1)[0]
		e1 := newMetric(map[string]string{"TESTTAG": "TESTTAGVALUE1"})
		assert.True(t, reflect.DeepEqual(r1.Tags(), e1.Tags()))

		time.Sleep(20 * time.Millisecond)

		m2 := newMetric(nil)
		r2 := tagger.Apply(m2)[0]
		e2 := newMetric(map[string]string{"TESTTAG": "TESTTAGVALUE1"})
		assert.True(t, reflect.DeepEqual(r2.Tags(), e2.Tags()))

		time.Sleep(10 * time.Millisecond)

		m3 := newMetric(nil)
		r3 := tagger.Apply(m3)[0]
		e3 := newMetric(map[string]string{"TESTTAG": "TESTTAGVALUE3"})
		assert.True(t, reflect.DeepEqual(r3.Tags(), e3.Tags()))
	})
	t.Run("Stop will stop refersh", func(t *testing.T) {
		em := new(MockEC2MetadataAPI)
		em.On("Available").Return(true)
		em.On("GetInstanceIdentityDocument").Return(ec2metadata.EC2InstanceIdentityDocument{Region: "TESTREGION"}, nil)

		e := new(MockEC2API)
		e.On("DescribeTags", mock.Anything).
			Return(&ec2.DescribeTagsOutput{Tags: []*ec2.TagDescription{}}, nil).
			Twice()

		p := new(MockEC2APIProvider)
		p.On("newEC2", mock.Anything, mock.Anything).Return(e)
		p.On("newEC2Metadata", mock.Anything, mock.Anything).Return(em)

		tagger := &Tagger{
			RefreshInterval:    internal.Duration{5 * time.Millisecond},
			EC2InstanceTagKeys: []string{"*"},
			ec2APIProvider:     p,
		}

		err := tagger.Start()
		assert.Nil(t, err)
		time.Sleep(10 * time.Millisecond)
		tagger.Stop()
		time.Sleep(10 * time.Millisecond)

		em.AssertExpectations(t)
		e.AssertExpectations(t)
		p.AssertExpectations(t)
	})
}

func TestInit(t *testing.T) {
	creator, ok := processors.Processors["ec2tagger"]
	assert.True(t, ok, "ec2tagger should be found in processors")
	processor := creator()
	tagger := processor.(*Tagger)
	assert.IsType(t, awsEC2APIProvider{}, tagger.ec2APIProvider)
}

func newMetric(tags map[string]string) telegraf.Metric {
	testFields := map[string]interface{}{"TESTFIELD": 0}
	m, _ := metric.New("TESTMETRIC", tags, testFields, time.Time{})
	return m
}
