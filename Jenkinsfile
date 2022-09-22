pipeline {
    agent {
        node {
            label 'golang-agent-v2'
        }
    }
    parameters {
        booleanParam defaultValue: false, description: 'Whether to upload the packages in playground repositories', name: 'PLAYGROUND'
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
        stage('Setup') {
            steps {
                sh '''
sudo bash -c 'echo "deb [trusted=yes] https://repo.zextras.io/rc/ubuntu focal main" > /etc/apt/sources.list.d/zextras.list'
'''
                sh 'sudo apt-get update && sudo apt-get install -y service-discover-base'
            }
        }
        stage('Stash') {
            steps {
                checkout scm
                stash includes: "**", name: 'project'
            }
        }
        stage('Tests') {
            steps {
                script {
                    sh 'rm -rfv /home/agent/.gnupg'
                    sh 'mkdir -p /home/agent/.gnupg'
                    def modules = [:]
                    def builds = [:]
                    modules["agent"] = "cli/agent"
                    modules["server"] = "cli/server"
                    modules["command"] = "cli/lib/command"
                    modules["credentialsEncrypter"] = "cli/lib/credentialsEncrypter"
                    modules["exec"] = "cli/lib/exec"
                    modules["formatter"] = "cli/lib/formatter"
                    modules["parser"] = "cli/lib/parser"
                    modules["zimbra"] = "cli/lib/zimbra"
                    modules.each{key, value ->
                        builds[key] = {
                            dir(value) {
                                sh 'gotestsum --format testname --junitfile tests.xml'
                                junit allowEmptyResults: false, checksName: "Test for " + key, testResults: 'tests.xml'
                            }
                        }
                    }
                    parallel builds
                }
            }
        }
        stage('Build deb/rpm') {
            parallel {
                stage('Ubuntu 20.04') {
                    agent {
                        node {
                            label 'pacur-agent-ubuntu-20.04-v1'
                        }
                    }
                    steps {
                        unstash 'project'
                        sh 'sudo cp -r * /tmp'
                        sh 'sudo pacur build ubuntu'
                        stash includes: 'artifacts/', name: 'artifacts-deb'
                    }
                    post {
                        always {
                            archiveArtifacts artifacts: "artifacts/*.deb", fingerprint: true
                        }
                    }
                }

                stage('Rocky 8') {
                    agent {
                        node {
                            label 'pacur-agent-rocky-8-v1'
                        }
                    }
                    steps {
                        unstash 'project'
                        sh 'sudo cp -r * /tmp'
                        sh 'sudo pacur build centos'
                        dir("artifacts/") {
                            sh 'echo service-discover-server* | sed -E "s#(service-discover-server-[0-9.]*).*#\\0 \\1.x86_64.rpm#" | xargs sudo mv'
                            sh 'echo service-discover-agent* | sed -E "s#(service-discover-agent-[0-9.]*).*#\\0 \\1.x86_64.rpm#" | xargs sudo mv'
                            sh 'echo service-discover-daemon* | sed -E "s#(service-discover-daemon-[0-9.]*).*#\\0 \\1.x86_64.rpm#" | xargs sudo mv'
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
        stage('Upload To Playground') {
            when {
                anyOf {
                    branch 'playground/*'
                    expression { params.PLAYGROUND == true }
                }
            }
            steps {
                unstash 'artifacts-deb'
                unstash 'artifacts-rpm'
                script {
                    def server = Artifactory.server 'zextras-artifactory'
                    def buildInfo
                    def uploadSpec

                    buildInfo = Artifactory.newBuildInfo()
                    uploadSpec = """{
                        "files": [
                            {
                                "pattern": "artifacts/service-discover-server*.deb",
                                "target": "ubuntu-playground/pool/",
                                "props": "centos8deb.distribution=focal;deb.component=main;deb.architecture=amd64"
                            },
                            {
                                "pattern": "artifacts/service-discover-agent*.deb",
                                "target": "ubuntu-playground/pool/",
                                "props": "centos8deb.distribution=focal;deb.component=main;deb.architecture=amd64"
                            },
                            {
                                "pattern": "artifacts/service-discover-daemon*.deb",
                                "target": "ubuntu-playground/pool/",
                                "props": "centos8deb.distribution=focal;deb.component=main;deb.architecture=amd64"
                            },
                             {
                                "pattern": "artifacts/(service-discover-server)-(*).rpm",
                                "target": "centos8-playground/zextras/{1}/{1}-{2}.rpm",
                                "props": "rpm.metadata.arch=x86_64;rpm.metadata.vendor=zextras"
                            },
                            {
                                "pattern": "artifacts/(service-discover-agent)-(*).rpm",
                                "target": "centos8-playground/zextras/{1}/{1}-{2}.rpm",
                                "props": "rpm.metadata.arch=x86_64;rpm.metadata.vendor=zextras"
                            },
                            {
                                "pattern": "artifacts/(service-discover-daemon)-(*).rpm",
                                "target": "centos8-playground/zextras/{1}/{1}-{2}.rpm",
                                "props": "rpm.metadata.arch=x86_64;rpm.metadata.vendor=zextras"
                            },
                            {
                                "pattern": "artifacts/(service-discover-server)-(*).rpm",
                                "target": "centos8-playground/zextras/{1}/{1}-{2}.rpm",
                                "props": "rpm.metadata.arch=x86_64;rpm.metadata.vendor=zextras"
                            },
                            {
                                "pattern": "artifacts/(service-discover-agent)-(*).rpm",
                                "target": "centos8-playground/zextras/{1}/{1}-{2}.rpm",
                                "props": "rpm.metadata.arch=x86_64;rpm.metadata.vendor=zextras"
                            },
                            {
                                "pattern": "artifacts/(service-discover-daemon)-(*).rpm",
                                "target": "centos8-playground/zextras/{1}/{1}-{2}.rpm",
                                "props": "rpm.metadata.arch=x86_64;rpm.metadata.vendor=zextras"
                            }
                        ]
                    }"""
                    server.upload spec: uploadSpec, buildInfo: buildInfo, failNoOp: false
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
                                "props": "centos8deb.distribution=focal;deb.component=main;deb.architecture=amd64"
                            },
                            {
                                "pattern": "artifacts/service-discover-agent*.deb",
                                "target": "ubuntu-rc/pool/",
                                "props": "centos8deb.distribution=focal;deb.component=main;deb.architecture=amd64"
                            },
                            {
                                "pattern": "artifacts/service-discover-daemon*.deb",
                                "target": "ubuntu-rc/pool/",
                                "props": "centos8deb.distribution=focal;deb.component=main;deb.architecture=amd64"
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
