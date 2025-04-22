package promscrape

import (
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/VictoriaMetrics/metrics"

	"github.com/VictoriaMetrics/VictoriaMetrics/lib/auth"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/logger"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/prompbmarshal"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/promscrape/discovery/azure"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/promscrape/discovery/consul"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/promscrape/discovery/digitalocean"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/promscrape/discovery/dns"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/promscrape/discovery/docker"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/promscrape/discovery/dockerswarm"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/promscrape/discovery/ec2"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/promscrape/discovery/eureka"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/promscrape/discovery/gce"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/promscrape/discovery/http"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/promscrape/discovery/kubernetes"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/promscrape/discovery/openstack"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/promscrape/discovery/yandexcloud"
)

type Scraper struct {
	globalStopCh         chan struct{}
	scraperWG            sync.WaitGroup
	configDetail         []byte
	PendingScrapeConfigs int32
	configData           atomic.Value

	name              string
	authorizationPath string
	pushData          func(at *auth.Token, wr *prompbmarshal.WriteRequest)
}

func NewScraper(configDetail []byte, name, authorizationPath string) *Scraper {
	return &Scraper{
		configDetail:      configDetail,
		name:              name,
		authorizationPath: authorizationPath,
	}
}

func (s *Scraper) Init(pushData func(at *auth.Token, wr *prompbmarshal.WriteRequest)) {
	mustInitClusterMemberID()
	s.globalStopCh = make(chan struct{})
	s.scraperWG.Add(1)
	s.pushData = pushData
	go func() {
		defer s.scraperWG.Done()
		s.runScraper()
	}()
}

func (s *Scraper) Stop() {
	close(s.globalStopCh)
	s.scraperWG.Wait()
}

func (s *Scraper) CheckConfig() error {
	_, err := loadContentConfig(s.configDetail, s.authorizationPath)
	return err
}

func (s *Scraper) runScraper() {
	if len(s.configDetail) == 0 {
		// Nothing to scrape.
		return
	}
	logger.Infof("reading Prometheus configs from %q", s.name)
	cfg, err := loadContentConfig(s.configDetail, s.authorizationPath)
	if err != nil {
		logger.Fatalf("cannot read %q: %s", s.name, err)
	}
	marshaledData := cfg.marshal()
	configData.Store(&marshaledData)
	cfg.mustStart()
	scs := newScrapeConfigs(s.pushData, s.globalStopCh)
	scs.add(s.name+"_azure_sd_configs", *azure.SDCheckInterval, func(cfg *Config, swsPrev []*ScrapeWork) []*ScrapeWork { return cfg.getAzureSDScrapeWork(swsPrev) })
	scs.add(s.name+"_consul_sd_configs", *consul.SDCheckInterval, func(cfg *Config, swsPrev []*ScrapeWork) []*ScrapeWork { return cfg.getConsulSDScrapeWork(swsPrev) })
	scs.add(s.name+"_digitalocean_sd_configs", *digitalocean.SDCheckInterval, func(cfg *Config, swsPrev []*ScrapeWork) []*ScrapeWork { return cfg.getDigitalOceanDScrapeWork(swsPrev) })
	scs.add(s.name+"_dns_sd_configs", *dns.SDCheckInterval, func(cfg *Config, swsPrev []*ScrapeWork) []*ScrapeWork { return cfg.getDNSSDScrapeWork(swsPrev) })
	scs.add(s.name+"_docker_sd_configs", *docker.SDCheckInterval, func(cfg *Config, swsPrev []*ScrapeWork) []*ScrapeWork { return cfg.getDockerSDScrapeWork(swsPrev) })
	scs.add(s.name+"_dockerswarm_sd_configs", *dockerswarm.SDCheckInterval, func(cfg *Config, swsPrev []*ScrapeWork) []*ScrapeWork { return cfg.getDockerSwarmSDScrapeWork(swsPrev) })
	scs.add(s.name+"_ec2_sd_configs", *ec2.SDCheckInterval, func(cfg *Config, swsPrev []*ScrapeWork) []*ScrapeWork { return cfg.getEC2SDScrapeWork(swsPrev) })
	scs.add(s.name+"_eureka_sd_configs", *eureka.SDCheckInterval, func(cfg *Config, swsPrev []*ScrapeWork) []*ScrapeWork { return cfg.getEurekaSDScrapeWork(swsPrev) })
	scs.add(s.name+"_file_sd_configs", *fileSDCheckInterval, func(cfg *Config, swsPrev []*ScrapeWork) []*ScrapeWork { return cfg.getFileSDScrapeWork(swsPrev) })
	scs.add(s.name+"_gce_sd_configs", *gce.SDCheckInterval, func(cfg *Config, swsPrev []*ScrapeWork) []*ScrapeWork { return cfg.getGCESDScrapeWork(swsPrev) })
	scs.add(s.name+"_http_sd_configs", *http.SDCheckInterval, func(cfg *Config, swsPrev []*ScrapeWork) []*ScrapeWork { return cfg.getHTTPDScrapeWork(swsPrev) })
	scs.add(s.name+"_kubernetes_sd_configs", *kubernetes.SDCheckInterval, func(cfg *Config, swsPrev []*ScrapeWork) []*ScrapeWork { return cfg.getKubernetesSDScrapeWork(swsPrev) })
	scs.add(s.name+"_openstack_sd_configs", *openstack.SDCheckInterval, func(cfg *Config, swsPrev []*ScrapeWork) []*ScrapeWork { return cfg.getOpenStackSDScrapeWork(swsPrev) })
	scs.add(s.name+"_yandexcloud_sd_configs", *yandexcloud.SDCheckInterval, func(cfg *Config, swsPrev []*ScrapeWork) []*ScrapeWork { return cfg.getYandexCloudSDScrapeWork(swsPrev) })
	scs.add(s.name+"_static_configs", 0, func(cfg *Config, swsPrev []*ScrapeWork) []*ScrapeWork { return cfg.getStaticScrapeWork() })

	scs.updateConfig(cfg)
	<-s.globalStopCh
	cfg.mustStop()
	logger.Infof("stopping Prometheus scrapers")
	startTime := time.Now()
	scs.stop()
	logger.Infof("stopped Prometheus scrapers in %.3f seconds", time.Since(startTime).Seconds())
	metrics.Clear(s.name)
}

// loadContentConfig loads Prometheus config from the configuration content.
func loadContentConfig(detail []byte, authorizationPath string) (*Config, error) {
	var cfgObj Config
	if err := cfgObj.unmarshal(detail, false); err != nil {
		return nil, fmt.Errorf("cannot unmarshal data: %w", err)
	}
	cfgObj.baseDir = filepath.Dir(authorizationPath)
	for i := range cfgObj.ScrapeConfigs {
		sc := cfgObj.ScrapeConfigs[i]
		swc, err := getScrapeWorkConfig(sc, cfgObj.baseDir, &cfgObj.Global)
		if err != nil {
			return nil, fmt.Errorf("cannot parse `scrape_config` #%d: %w", i+1, err)
		}
		sc.swc = swc
	}
	return &cfgObj, nil
}

func ConfigMemberInfo(total int, number string) {
	*clusterMemberNum = number
	*clusterMembersCount = total
	*noStaleMarkers = true
}
