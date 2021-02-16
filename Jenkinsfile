pipeline {
    agent {
        node {
            label 'golang-agent-v2'
        }
    }
    options {
        skipDefaultCheckout()
        buildDiscarder(logRotator(numToKeepStr: '25'))
        timeout(time: 2, unit: 'HOURS')
    }
    environment {
        GOPRIVATE="gitlab.com/zextras,bitbucket.org/zextras,github.com/zextras"
    }
    stages {
        stage('Stash') {
            steps {
                checkout scm
                stash includes: "**", name: 'project'
            }
        }
        stage('Tests') {
            steps {
                script {
                    def modules = [:]
                    def builds = [:]
                    modules["agent"] = "cli/agent"
                    modules["server"] = "cli/server"
                    modules["parser"] = "cli/lib/parser"
                    modules["formatter"] = "cli/lib/formatter"
                    modules["command"] = "cli/lib/command"
                    modules.each{key, value ->
                        builds[key] = {
                            dir(value) {
                                sh 'gotestsum --format testname --junitfile tests.xml || true'
                                junit checksName: "Test for " + key, testResults: 'tests.xml'
                            }
                        }
                    }
                    parallel builds
                }
            }
        }
        stage('Build deb/rpm') {
            parallel {
                stage('Ubuntu 16.04') {
                    agent {
                        node {
                            label 'pacur-agent-ubuntu-16.04-v1'
                        }
                    }
                    steps {
                        unstash 'project'
                        sh 'sudo mkdir /repo/'
                        sh 'sudo mv * /repo/'

                        sh 'cp /repo/cli/server/PKGBUILD /pacur/'
                        sh 'sudo pacur build ubuntu'

                        sh 'cp /repo/cli/agent/PKGBUILD /pacur/'
                        sh 'sudo pacur build ubuntu'

                        sh 'cp /repo/service-discoverd/PKGBUILD /pacur/'
                        sh 'sudo pacur build ubuntu'

                        sh 'mkdir artifacts/'
                        sh 'sudo cp /pacur/service-discover-server*.deb artifacts/'
                        sh 'sudo cp /pacur/service-discover-agent*.deb artifacts/'
                        sh 'sudo cp /pacur/service-discover-daemon*.deb artifacts/'
                        dir("artifacts/") {
                            sh 'echo service-discover-server* | sed -E "s#(service-discover-server_[0-9.]*).*#\\0 \\1_amd64.deb#" | xargs mv'
                            sh 'echo service-discover-agent* | sed -E "s#(service-discover-agent_[0-9.]*).*#\\0 \\1_amd64.deb#" | xargs mv'
                            sh 'echo service-discover-daemon* | sed -E "s#(service-discover-daemon_[0-9.]*).*#\\0 \\1_amd64.deb#" | xargs mv'
                        }
                        stash includes: 'artifacts/', name: 'artifacts-deb'
                    }
                    post {
                        always {
                            archiveArtifacts artifacts: "artifacts/*.deb", fingerprint: true
                        }
                    }
                }

                stage('Centos 7') {
                    agent {
                        node {
                            label 'pacur-agent-centos-7-v1'
                        }
                    }
                    steps {
                        unstash 'project'
                        sh 'sudo mkdir /repo/'
                        sh 'sudo mv * /repo/'

                        sh 'cp /repo/cli/server/PKGBUILD /pacur/'
                        sh 'sudo pacur build centos'

                        sh 'cp /repo/cli/agent/PKGBUILD /pacur/'
                        sh 'sudo pacur build centos'

                        sh 'cp /repo/service-discoverd/PKGBUILD /pacur/'
                        sh 'sudo pacur build centos'

                        sh 'mkdir artifacts/'
                        sh 'sudo cp /pacur/service-discover-server*.rpm artifacts/'
                        sh 'sudo cp /pacur/service-discover-agent*.rpm artifacts/'
                        sh 'sudo cp /pacur/service-discover-daemon*.rpm artifacts/'
                        dir("artifacts/") {
                            sh 'echo service-discover-server* | sed -E "s#(service-discover-server-[0-9.]*).*#\\0 \\1.x86_64.rpm#" | xargs mv'
                            sh 'echo service-discover-agent* | sed -E "s#(service-discover-agent-[0-9.]*).*#\\0 \\1.x86_64.rpm#" | xargs mv'
                            sh 'echo service-discover-daemon* | sed -E "s#(service-discover-daemon-[0-9.]*).*#\\0 \\1.x86_64.rpm#" | xargs mv'
                        }
                        stash includes: 'artifacts/', name: 'artifacts-rpm'
                    }
                    post {
                        always {
                            archiveArtifacts artifacts: "artifacts/*.rpm", fingerprint: true
                        }
                    }
                }
            }
        }
        stage('Upload & Promotion Config') {
            when {
                buildingTag()
            }
            steps {
                unstash 'artifacts-deb'
                unstash 'artifacts-rpm'
                script {
                    def server = Artifactory.server 'zextras-artifactory'
                    def buildInfo
                    def uploadSpec
                    def config

                    //ubuntu
                    buildInfo = Artifactory.newBuildInfo()
                    buildInfo.name += "-ubuntu"
                    uploadSpec= """{
                        "files": [
                            {
                                "pattern": "artifacts/service-discover-server*.deb",
                                "target": "ubuntu-rc/pool/",
                                "props": "deb.distribution=xenial;deb.distribution=bionic;deb.distribution=focal;deb.component=main;deb.architecture=amd64"
                            },
                            {
                                "pattern": "artifacts/service-discover-agent*.deb",
                                "target": "ubuntu-rc/pool/",
                                "props": "deb.distribution=xenial;deb.distribution=bionic;deb.distribution=focal;deb.component=main;deb.architecture=amd64"
                            },
                            {
                                "pattern": "artifacts/service-discover-daemon*.deb",
                                "target": "ubuntu-rc/pool/",
                                "props": "deb.distribution=xenial;deb.distribution=bionic;deb.distribution=focal;deb.component=main;deb.architecture=amd64"
                            }
                        ]
                    }"""
                    server.upload spec: uploadSpec, buildInfo: buildInfo, failNoOp: false
                    config = [
                            'buildName'          : buildInfo.name,
                            'buildNumber'        : buildInfo.number,
                            'sourceRepo'         : 'ubuntu-rc',
                            'targetRepo'         : 'ubuntu-release',
                            'comment'            : 'Do not change anything! Just press the button',
                            'status'             : 'Released',
                            'includeDependencies': false,
                            'copy'               : true,
                            'failFast'           : true
                    ]
                    Artifactory.addInteractivePromotion server: server, promotionConfig: config, displayName: "Ubuntu Promotion to Release"
                    server.publishBuildInfo buildInfo

                    //centos7
                    buildInfo = Artifactory.newBuildInfo()
                    buildInfo.name += "-centos7"
                    uploadSpec= """{
                        "files": [
                            {
                                "pattern": "artifacts/(service-discover-server)-(*).rpm",
                                "target": "centos7-rc/zextras/{1}/{1}-{2}.rpm",
                                "props": "rpm.metadata.arch=x86_64;rpm.metadata.vendor=zextras"
                            },
                            {
                                "pattern": "artifacts/(service-discover-agent)-(*).rpm",
                                "target": "centos7-rc/zextras/{1}/{1}-{2}.rpm",
                                "props": "rpm.metadata.arch=x86_64;rpm.metadata.vendor=zextras"
                            },
                            {
                                "pattern": "artifacts/(service-discover-daemon)-(*).rpm",
                                "target": "centos7-rc/zextras/{1}/{1}-{2}.rpm",
                                "props": "rpm.metadata.arch=x86_64;rpm.metadata.vendor=zextras"
                            }
                        ]
                    }"""
                    server.upload spec: uploadSpec, buildInfo: buildInfo, failNoOp: false
                    config = [
                            'buildName'          : buildInfo.name,
                            'buildNumber'        : buildInfo.number,
                            'sourceRepo'         : 'centos7-rc',
                            'targetRepo'         : 'centos7-release',
                            'comment'            : 'Do not change anything! Just press the button',
                            'status'             : 'Released',
                            'includeDependencies': false,
                            'copy'               : true,
                            'failFast'           : true
                    ]
                    Artifactory.addInteractivePromotion server: server, promotionConfig: config, displayName: "Centos7 Promotion to Release"
                    server.publishBuildInfo buildInfo

                    //centos8
                    buildInfo = Artifactory.newBuildInfo()
                    buildInfo.name += "-centos8"
                    uploadSpec= """{
                        "files": [
                            {
                                "pattern": "artifacts/(service-discover-server)-(*).rpm",
                                "target": "centos8-rc/zextras/{1}/{1}-{2}.rpm",
                                "props": "rpm.metadata.arch=x86_64;rpm.metadata.vendor=zextras"
                            },
                            {
                                "pattern": "artifacts/(service-discover-agent)-(*).rpm",
                                "target": "centos8-rc/zextras/{1}/{1}-{2}.rpm",
                                "props": "rpm.metadata.arch=x86_64;rpm.metadata.vendor=zextras"
                            },
                            {
                                "pattern": "artifacts/(service-discover-daemon)-(*).rpm",
                                "target": "centos8-rc/zextras/{1}/{1}-{2}.rpm",
                                "props": "rpm.metadata.arch=x86_64;rpm.metadata.vendor=zextras"
                            }
                        ]
                    }"""
                    server.upload spec: uploadSpec, buildInfo: buildInfo, failNoOp: false
                    config = [
                            'buildName'          : buildInfo.name,
                            'buildNumber'        : buildInfo.number,
                            'sourceRepo'         : 'centos8-rc',
                            'targetRepo'         : 'centos8-release',
                            'comment'            : 'Do not change anything! Just press the button',
                            'status'             : 'Released',
                            'includeDependencies': false,
                            'copy'               : true,
                            'failFast'           : true
                    ]
                    Artifactory.addInteractivePromotion server: server, promotionConfig: config, displayName: "Centos8 Promotion to Release"
                    server.publishBuildInfo buildInfo

                }
            }
        }
    }
    post {
        always {
            script {
                GIT_COMMIT_EMAIL = sh(
                    script: 'git --no-pager show -s --format=\'%ae\'',
                    returnStdout: true
                ).trim()
            }
            emailext attachLog: true, body: '$DEFAULT_CONTENT', recipientProviders: [requestor()], subject: '$DEFAULT_SUBJECT', to: "${GIT_COMMIT_EMAIL}"
            junit allowEmptyResults: true, testResults: 'test-out/**/*.xml'
        }
    }
}
