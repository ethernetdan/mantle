package tectonic

import (
	"github.com/coreos/pkg/capnslog"

	"github.com/coreos/mantle/kola/register"
)

// platforms supported by tectonic
var supportedPlatforms = []string{"gce"}

var plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "kola/tests/etcd")

func init() {
	registerBootkube()
}

func registerBootkube() {
	masterInit := renderBootkubeInit(true, nil)

	register.Register(&register.Test{
		Name:      "coreos.tectonic.bootkube-simple",
		Run:       BootkubeSimple,
		Platforms: supportedPlatforms,

		ClusterSize: 1,
		UserData:    masterInit,
	})
}
