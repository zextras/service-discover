source "vagrant" "agent-ubuntu18" {
  communicator = "ssh"
  source_path = "hashicorp/bionic64"
  box_name = "zextras/service-discover-agent"
  provider = "virtualbox"
}

build {
  sources = ["source.vagrant.agent-ubuntu18"]

  provisioner "shell" {
    execute_command = "echo 'packer' | sudo -S sh -c '{{ .Vars }} {{ .Path }}'"
    script = "misc/agent.ubuntu18.provisioner.sh"
  }
}
