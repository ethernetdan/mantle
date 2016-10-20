package tectonic

import (
	"bytes"
	"fmt"
	"net/url"
	"path/filepath"
	"text/template"

	bootkube "github.com/kubernetes-incubator/bootkube/pkg/asset"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/tests/etcd"
	"github.com/coreos/mantle/platform"
)

const (
	// kubeletVersion used on each Node
	kubeletVersion = "v1.4.0_coreos.0"

	// bootkubeAssetDir is the directory which bootkube uses for assets
	bootkubeAssetDir = "/home/core/cluster"
)

// BootkubeSimple brings up a multi node bootkube cluster with static etcd.
func BootkubeSimple(c cluster.TestCluster) error {
	initialMachines := len(c.Machines())
	if initialMachines != 1 {
		return fmt.Errorf("expected to have 1 initial machines, have %d", initialMachines)
	}
	master := c.Machines()[0]

	// wait until first cluster member comes up
	plog.Info("Waiting for bootkube master to come up")
	if err := etcd.GetClusterHealth(master, 1); err != nil {
		return fmt.Errorf("failed to start bootkube master: %v", err)
	}

	// generate bootkube assets
	config, err := bootkubeConfigFromMaster(master)
	if err != nil {
		return fmt.Errorf("failed to create bootkube config: %v", err)
	}
	assets, err := bootkube.NewDefaultAssets(config)
	if err != nil {
		return fmt.Errorf("failed to create bootkube assets: %v", err)
	}

	// write master assets
	if err = provisionAssets(master, assets, bootkubeAssetDir); err != nil {
		return fmt.Errorf("failed to provision bootkube assets to the master: %v", err)
	}

	//startCmd := fmt.Sprintf("sudo /home/core/bootkube start --asset-dir=%s --etcd-server=%s 2>> /home/core/bootkube.log", bootkubeAssetDir, config.EtcdServers[0].String())
	return nil
}

func renderBootkubeInit(master bool, kubeconfig []byte) string {
	config := struct {
		Master         bool
		KubeletVersion string
		Kubeconfig     string
	}{
		master,
		kubeletVersion,
		string(kubeconfig),
	}

	buf := new(bytes.Buffer)
	tmpl := template.Must(template.New("bootkubeInit").Parse(cloudConfigTmpl))
	tmpl.Execute(buf, &config)
	return buf.String()
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
	var dirs map[string]bool
	for _, a := range assets {
		dst := filepath.Join(dstDir, a.Name)
		dirs[filepath.Dir(dst)] = true
	}
	// create directories
	for dir := range dirs {
		if err := mkDir(dir); err != nil {
			return fmt.Errorf("could not create directory '%s': %v", dir, err)
		}
	}

	// write assets
	for _, asset := range assets {
		dst := filepath.Join(dstDir, asset.Name)
		assetReader := bytes.NewReader(asset.Data)
		if err := platform.InstallFile(assetReader, m, dst); err != nil {
			return fmt.Errorf("failed to install asset '%s': %v", dst, err)
		}
	}
	return nil
}

func startWorkers(c cluster.TestCluster, assets bootkube.Assets, count int) ([]platform.Machine, error) {
	kubeconfig, err := assets.Get(bootkube.AssetPathKubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed retrieve kubeconfig from assets: %v", err)
	}
	workerInit := renderBootkubeInit(false, kubeconfig.Data)

	userdatas := make([]string, count)
	for i := 0; i < count; i++ {
		userdatas[i] = workerInit
	}

	return platform.NewMachines(c, userdatas)
}

func bootkubeConfigFromMaster(m platform.Machine) (bootkube.Config, error) {
	etcdURL, err := createURL("http", m.IP(), 2379)
	if err != nil {
		return bootkube.Config{}, fmt.Errorf("could not validate etcd URL: %v", err)
	}

	masterURL, err := createURL("https", m.IP(), 443)
	if err != nil {
		return bootkube.Config{}, fmt.Errorf("could not validate bootkube master URL: %v", err)
	}

	return bootkube.Config{
		EtcdServers: []*url.URL{etcdURL},
		APIServers:  []*url.URL{masterURL},
	}, nil
}

func createURL(proto, host string, port int) (*url.URL, error) {
	str := fmt.Sprintf("%s://%s:%d", proto, host, port)
	return url.Parse(str)
}
