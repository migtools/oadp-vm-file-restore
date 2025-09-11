## Kubebuilder

The project was generated using kubebuilder version `v3.33.0`, running the following commands
```sh
kubebuilder init \
    --plugins go.kubebuilder.io/v4 \
    --project-version 3 \
    --project-name=oadp-vm-file-restore \
    --repo=github.com/migtools/oadp-vm-file-restore \
    --domain=openshift.io
kubebuilder create api \
    --plugins go.kubebuilder.io/v4 \
    --group oadp \
    --version v1alpha1 \
    --kind VirtualMachineFileRestore \
    --resource --controller
make manifests
```
