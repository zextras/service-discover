PACKER_FOLDER_OUTPUT_AGENT = "output-agent-ubuntu18"
PACKER_FOLDER_OUTPUT_SERVER = "output-server-ubuntu18"
SHELL := /bin/bash

vagrant-up:
	@if [[ ! -d ${PACKER_FOLDER_OUTPUT_SERVER} ]]; \
	then \
  	  packer build server.ubuntu18.pkr.hcl; \
  	  vagrant box add --force --name zextras/service-discover-server output-server-ubuntu18/package.box; \
  	fi; \
  	if [[ ! -d ${PACKER_FOLDER_OUTPUT_AGENT} ]]; \
  	then \
  	  packer build agent.ubuntu18.pkr.hcl; \
  	  vagrant box add --force --name zextras/service-discover-agent output-agent-ubuntu18/package.box; \
  	fi; \
  	vagrant up; \
  	echo "You instance is ready. Note service-discover cli is the one coming from deb packages."; \
  	echo "If you want to use just compiled service-discover binary, copy them from /vagrant folder, that is bound to the host"

vagrant-destroy:
	@vagrant destroy --force

clean:
	@rm -rf $$PACKER_FOLDER_OUTPUT_AGENT && rm -rf $$PACKER_FOLDER_OUTPUT_SERVER