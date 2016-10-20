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
  units:{{ if .Master }}
    - name: etcd2.service
      command: start
      runtime: true{{ end }}
    - name: kubelet.service
      enable: true
      command: start
      content: |
        [Service]
        EnvironmentFile=/etc/environment
        Environment=KUBELET_ACI=quay.io/coreos/hyperkube
        Environment=KUBELET_VERSION={{ .KubeletVersion }}
        ExecStartPre=/bin/mkdir -p /etc/kubernetes/manifests
        ExecStartPre=/bin/mkdir -p /srv/kubernetes/manifests
        ExecStartPre=/bin/mkdir -p /etc/kubernetes/checkpoint-secrets
        ExecStart=/usr/lib/coreos/kubelet-wrapper \
          --kubeconfig=/etc/kubernetes/kubeconfig \
          --require-kubeconfig \
          --lock-file=/var/run/lock/kubelet.lock \
          --exit-on-lock-contention \
          --pod-manifest-path=/etc/kubernetes/manifests \
          --allow-privileged \
          --hostname-override=${COREOS_PUBLIC_IPV4} \{{ if .Master }}
          --node-labels=master=true \{{ end }}
          --cluster_dns=10.3.0.10 \
          --cluster_domain=cluster.local
        Restart=always
        RestartSec=5

        [Install]
        WantedBy=multi-user.target
{{ if !.Master }}
write_files:
  - path: "/etc/kubernetes/kubeconfig"
    permissions: "0644"
    owner: core
    content: |
{{ .Kubeconfig }}
{{ end }}
`
