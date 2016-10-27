package tectonic

import (
	"bytes"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"text/template"

	bootkube "github.com/kubernetes-incubator/bootkube/pkg/asset"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/tests/etcd"
	"github.com/coreos/mantle/platform"
	"github.com/kubernetes-incubator/bootkube/pkg/tlsutil"
)

const (
	// kubeletVersion used on each Node
	kubeletVersion = "latest"

	// bootkubeRootDir is the directory containing bootkube and the bootkubeAssetDir
	bootkubeRootDir = "/home/core"

	// localBootkubePath is the path of the bootkube executable to be transferred to each node
	localBootkubePath = "./data/bootkube"

	// kubeconfigPath is the path to the kubelet configuration for each node.
	kubeconfigPath = "/etc/kubernetes/kubeconfig"

	// numWorkers is the number of worker Nodes that should be spawned.
	numWorkers = 0
)

var (
	// remoteBootkubePath is the path of where the bootkube executable is provisioned
	remoteBootkubePath = filepath.Join(bootkubeRootDir, "bootkube")

	// bootkubeAssetDir is the path bootkube assets are provisioned
	bootkubeAssetDir = filepath.Join(bootkubeRootDir, "cluster")
)

// BootkubeSimple brings up a multi node bootkube cluster with static etcd.
func BootkubeSimple(c cluster.TestCluster) error {
	initialMachines := len(c.Machines())
	if initialMachines != 1 {
		return fmt.Errorf("expected to have 1 initial machines, have %d", initialMachines)
	}
	master := c.Machines()[0]
	plog.Infof("Master VM (%s) started. It's IP is %s.", master.ID(), master.IP())

	plog.Infof("Creating bootkube config from master (%s)", master.IP())
	config, err := bootkubeConfigFromMaster(master)
	if err != nil {
		return fmt.Errorf("failed to create bootkube assets: %v", err)
	}

	plog.Info("Generating assets from config")
	assets, err := bootkube.NewDefaultAssets(config)
	if err != nil {
		return fmt.Errorf("failed to create bootkube assets from master config: %v", err)
	}

	plog.Info("Provisioning bootkube executable on master")
	if err = provisionBootkube(master); err != nil {
		return fmt.Errorf("failed to provision bootkube to the master: %v", err)
	}

	plog.Info("Provisioning bootkube assets on master")
	if err = provisionAssets(master, assets, bootkubeAssetDir); err != nil {
		return fmt.Errorf("failed to provision assets to master (%s): %v", master.IP(), err)
	}

	plog.Info("Waiting for master etcd to come up")
	if err = etcd.GetClusterHealth(master, 1); err != nil {
		return fmt.Errorf("failed to start bootkube master: %v", err)
	}

	plog.Info("Starting master bootkube")
	if err = startBootkube(master, bootkubeAssetDir, config.EtcdServers[0].String(), ""); err != nil {
		return fmt.Errorf("failed to start master bootkube (%s): %v", master.IP(), err)
	}

	var workers []platform.Machine
	if numWorkers != 0 {
		plog.Infof("Creating %d worker nodes...", numWorkers)
		workers, err = provisionWorkerMachines(c, master, numWorkers)
		if err != nil {
			return fmt.Errorf("error creating worker nodes: %v", err)
		}
	}

	for _, w := range workers {
		plog.Infof("Provisioning bootkube executable on worker %s (%s)", w.ID(), w.IP())
		if err = provisionBootkube(w); err != nil {
			return fmt.Errorf("failed to provision bootkube to the worker %s (%s): %v", w.ID(), w.IP(), err)
		}

		plog.Infof("Provisioning bootkube assets on worker %s (%s)", w.ID(), w.IP())
		if err = provisionAssets(w, assets, bootkubeAssetDir); err != nil {
			return fmt.Errorf("failed to provision assets to worker %s (%s): %v", w.ID(), w.IP(), err)
		}

		plog.Infof("Starting worker %s (%s) bootkube", w.ID(), w.IP())
		if err = startBootkube(w, bootkubeAssetDir, config.EtcdServers[0].String(), ""); err != nil {
			return fmt.Errorf("failed to start bootkube worker %s (%s): %v", w.ID(), w.IP(), err)
		}

	}

	closeChan := make(chan os.Signal, 1)
	signal.Notify(closeChan, os.Interrupt)
	<-closeChan

	return nil
}

func renderBootkubeInit(master string) string {
	isMaster := len(master) == 0
	flannelEtcd := master
	if isMaster {
		flannelEtcd = "http://127.0.0.1:2379"
	}
	config := struct {
		Master         bool
		KubeletVersion string
		Kubeconfig     string
		FlannelEtcd    string
	}{
		isMaster,
		kubeletVersion,
		kubeconfigPath,
		flannelEtcd,
	}

	buf := new(bytes.Buffer)
	tmpl := template.Must(template.New("bootkubeInit").Parse(cloudConfigTmpl))
	tmpl.Execute(buf, &config)
	return buf.String()
}

func provisionBootkube(m platform.Machine) error {
	f, err := os.Open(localBootkubePath)
	if err != nil {
		return fmt.Errorf("could not access the path (%s) of the local bootkube executable: %v", localBootkubePath, err)
	}
	defer f.Close()

	if err = platform.InstallFile(f, m, remoteBootkubePath); err != nil {
		return fmt.Errorf("failed to install bootkube executable to '%s' on %s (%s): %v", remoteBootkubePath, m.ID(), m.IP(), err)
	}
	return nil
}

func provisionAssets(m platform.Machine, assets bootkube.Assets, dstDir string) error {
	mkDir := func(dir string) error {
		cmd := fmt.Sprintf("mkdir -p %s", dir)
		if _, err := m.SSH(cmd); err != nil {
			return fmt.Errorf("failed to create directory '%s': %v", dir, err)
		}
		return nil
	}

	// populate required directories
	dirs := map[string]bool{}
	for _, a := range assets {
		dst := filepath.Join(dstDir, a.Name)
		dirs[filepath.Dir(dst)] = true
	}
	// create directories
	for dir := range dirs {
		if err := mkDir(dir); err != nil {
			return err
		}
	}

	// write assets
	for _, asset := range assets {
		dst := filepath.Join(dstDir, asset.Name)

		// write kubeconfig to system kubelet accessible directory
		if asset.Name == bootkube.AssetPathKubeConfig {
			dst = kubeconfigPath
		}

		assetReader := bytes.NewReader(asset.Data)
		if err := platform.InstallFile(assetReader, m, dst); err != nil {
			return fmt.Errorf("failed to install asset '%s': %v", dst, err)
		}
	}
	return nil
}

func startBootkube(m platform.Machine, assetDir, etcdServer, logFile string) error {
	if len(logFile) == 0 {
		logFile = remoteBootkubePath + ".log"
	}
	startCmd := fmt.Sprintf(`sudo nohup bash -c "%s start --asset-dir=%s --etcd-server=%s &" >>%s 2>&1`, remoteBootkubePath, assetDir, etcdServer, logFile)
	plog.Infof("Starting bootkube on %s with: %s", m.IP(), startCmd)
	out, err := m.SSH(startCmd)
	if err != nil {
		return fmt.Errorf("could not start bootkube on %s (%s): %v", m.ID(), m.IP(), err)
	}
	plog.Infof("Output from starting bootkube on %s: %s", m.IP(), out)
	return nil
}

func provisionWorkerMachines(c cluster.TestCluster, master platform.Machine, count int) ([]platform.Machine, error) {
	flannelMaster := fmt.Sprintf("http://%s:2379", master.IP())
	workerInit := renderBootkubeInit(flannelMaster)

	userdatas := make([]string, count)
	for i := 0; i < count; i++ {
		userdatas[i] = workerInit
	}

	return platform.NewMachines(c, userdatas)
}

func bootkubeConfigFromMaster(m platform.Machine) (bootkube.Config, error) {
	etcdURL, err := createURL("http", "127.0.0.1", 2379)
	if err != nil {
		return bootkube.Config{}, fmt.Errorf("could not validate etcd URL: %v", err)
	}

	masterURL, err := createURL("https", m.IP(), 443)
	if err != nil {
		return bootkube.Config{}, fmt.Errorf("could not validate bootkube master URL: %v", err)
	}

	apiServers := []*url.URL{masterURL}
	return bootkube.Config{
		EtcdServers: []*url.URL{etcdURL},
		APIServers:  apiServers,
		AltNames:    altNamesFromURLs(apiServers),
	}, nil
}

func createURL(proto, host string, port int) (*url.URL, error) {
	str := fmt.Sprintf("%s://%s:%d", proto, host, port)
	return url.Parse(str)
}

func altNamesFromURLs(urls []*url.URL) *tlsutil.AltNames {
	var an tlsutil.AltNames
	for _, u := range urls {
		host, _, err := net.SplitHostPort(u.Host)
		if err != nil {
			host = u.Host
		}
		ip := net.ParseIP(host)
		if ip == nil {
			an.DNSNames = append(an.DNSNames, host)
		} else {
			an.IPs = append(an.IPs, ip)
		}
	}
	return &an
}
