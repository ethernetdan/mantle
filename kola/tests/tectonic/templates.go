package tectonic

var cloudConfigTmpl = `#cloud-config
coreos: {{ if .Master }}
  etcd2:
    name: controller
    advertise-client-urls: http://$private_ipv4:2379
    initial-advertise-peer-urls: http://$private_ipv4:2380
    listen-client-urls: http://0.0.0.0:2379
    listen-peer-urls: http://0.0.0.0:2380
    initial-cluster: controller=http://$private_ipv4:2380{{ end }}
  flannel:
    etcd_endpoints: {{ .FlannelEtcd }}
  units: {{ if .Master }}
    - name: etcd2.service
      command: start
      runtime: true{{ end }}
    - name: flanneld.service
      command: start
      drop-ins:
      - name: 50-network-config.conf
        content: |
          [Service]
          ExecStartPre=/usr/bin/etcdctl --endpoint={{ .FlannelEtcd }} set /coreos.com/network/config '{ "Network": "10.2.0.0/16" }'
    - name: docker.service
      drop-ins:
      - name: 50-flannel.conf
        content: |
          [Unit]
          Requires=flanneld.service
          After=flanneld.service
    - name: kubelet.service
      command: start
      content: |
        [Service]
        EnvironmentFile=/etc/environment
        Environment=KUBELET_ACI=quay.io/dan_gillespie/hyperkube-amd64
        Environment=KUBELET_VERSION={{ .KubeletVersion }}
        ExecStartPre=/bin/mkdir -p /etc/kubernetes/manifests
        ExecStartPre=/bin/mkdir -p /srv/kubernetes/manifests
        ExecStartPre=/bin/mkdir -p /etc/kubernetes/checkpoint-secrets
        ExecStart=/usr/lib/coreos/kubelet-wrapper \
          --kubeconfig={{ .Kubeconfig }} \
          --require-kubeconfig \
          --lock-file=/var/run/lock/kubelet.lock \
          --exit-on-lock-contention \
          --pod-manifest-path=/etc/kubernetes/manifests \
          --allow-privileged \ {{ if .Master }}
          --node-labels=master=true \{{ end }}
          --register-node=true \
          --v=4 \
          --cluster_dns=10.3.0.10 \
          --cluster_domain=cluster.local
        Restart=always
        RestartSec=5

        [Install]
        WantedBy=multi-user.target
`
