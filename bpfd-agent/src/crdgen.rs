use bpfd_k8s_api::v1alpha1::EbpfProgram;
use kube::CustomResourceExt;

fn main() {
    print!("{}", serde_yaml::to_string(&EbpfProgram::crd()).unwrap())
}
