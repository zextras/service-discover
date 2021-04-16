source "vagrant" "server-ubuntu18" {
  communicator = "ssh"
  source_path = "hashicorp/bionic64"
  box_name = "zextras/service-discover-server"
  provider = "virtualbox"
  template = "misc/server-ubuntu18.template"
}

build {
  sources = ["source.vagrant.server-ubuntu18"]

  provisioner "file" {
    source = "misc/zinstall.service"
    destination = "/tmp/"
  }

  provisioner "file" {
    source = "misc/imahuman.sh"
    destination = "/tmp/"
  }

  provisioner "file" {
    source = "misc/zinstaller.sh"
    destination = "/tmp/"
  }

  provisioner "shell" {
    execute_command = "echo 'packer' | sudo -S sh -c '{{ .Vars }} {{ .Path }}'"
    script = "misc/server.ubuntu18.provisioner.sh"
  }
}