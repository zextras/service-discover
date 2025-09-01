pipeline {
    agent {
        node {
            label 'golang-v1'
        }
    }
    parameters {
        booleanParam defaultValue: false, description: 'Set to true to skip the test stage', name: 'SKIP_TEST'
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
                script {
                    env.GIT_COMMIT = sh(script: 'git rev-parse HEAD', returnStdout: true).trim()
                }
                stash includes: "**", name: 'project'
            }
        }
        stage('Tests') {
            when { expression { params.SKIP_TEST != true } }
            steps {
                container('dind') {
                    withDockerRegistry(credentialsId: 'private-registry', url: 'https://registry.dev.zextras.com') {
                        sh 'docker pull registry.dev.zextras.com/dev/carbonio-openldap:latest'
                    }
                }
                container('golang') {
                    script {
                        // WARNING: these tests need an integration environment
                        // They expect service-discover-base to be in the system
                        sh 'apt update && apt clean'
                        sh 'apt-key adv --keyserver hkp://keyserver.ubuntu.com:80 --recv-keys 52FD40243E584A21'
                        sh 'echo deb https://repo.zextras.io/release/ubuntu jammy main > /etc/apt/sources.list.d/zextras.list'
                        sh 'apt-get update && apt-get install -y service-discover-base'
                        def modules = [:]
                        def builds = [:]
                        modules["encrypter"] = "pkg/encrypter"
                        modules["exec"] = "pkg/exec"
                        modules["formatter"] = "pkg/formatter"
                        modules["parser"] = "pkg/parser"
                        modules["carbonio"] = "pkg/carbonio"
                        modules.each { key, value ->
                            builds[key] = {
                                dir(value) {
                                    sh 'go run gotest.tools/gotestsum@latest --format testname --junitfile tests.xml'
                                    junit allowEmptyResults: false, checksName: "Test for " + key, testResults: 'tests.xml'
                                }
                            }
                        }

                        parallel builds
                        dir('pkg/command') {
                            sh 'go run gotest.tools/gotestsum@latest --format testname --junitfile tests.xml'
                            junit allowEmptyResults: false, checksName: "Test for command", testResults: 'tests.xml'
                        }
                        dir('cmd/agent') {
                            sh 'go run gotest.tools/gotestsum@latest --format testname --junitfile tests.xml'
                            junit allowEmptyResults: false, checksName: "Test for agent", testResults: 'tests.xml'
                        }
                        dir('cmd/server') {
                            sh 'go run gotest.tools/gotestsum@latest --format testname --junitfile tests.xml'
                            junit allowEmptyResults: false, checksName: "Test for server", testResults: 'tests.xml'
                        }
                    }
                }
            }
        }
        stage('SonarQube analysis') {
            steps {
                container('golangci-lint') {
                    unstash 'project'
                    script {
                        scannerHome = tool 'SonarScanner';
                    }
                    sh 'golangci-lint run ./... --issues-exit-code 0 --output.checkstyle.path linter.out'
                }
                withSonarQubeEnv(credentialsId: 'sonarqube-user-token',
                    installationName: 'SonarQube instance') {
                    sh "${scannerHome}/bin/sonar-scanner"
                }
            }
        }
        stage('Build Ubuntu') {
            agent {
                node {
                    label 'yap-ubuntu-20-v1'
                }
            }
            steps {
                container('yap') {
                    unstash 'project'
                    sh 'mkdir -p /tmp/service-discover'
                    sh 'cp -r * /tmp/service-discover'
                    script {
                        sh "yap prepare ubuntu -g"
                        if (BRANCH_NAME == 'devel') {
                            def timestamp = new Date().format('yyyyMMddHHmmss')
                            sh "yap build ubuntu build -r ${timestamp} -sd"
                        } else {
                            sh 'yap build ubuntu build -sd'
                        }
                    }
                    stash includes: 'artifacts/*.deb', name: 'artifacts-ubuntu'
                }
            }
            post {
                always {
                    archiveArtifacts artifacts: "artifacts/*.deb", fingerprint: true
                }
            }
        }
        stage('Build RHEL') {
            agent {
                node {
                    label 'yap-rocky-8-v1'
                }
            }
            steps {
                container('yap') {
                    unstash 'project'
                    sh 'mkdir -p /tmp/service-discover'
                    sh 'cp -r * /tmp/service-discover'
                    script {
                        sh "sudo yap prepare rocky -g"
                        if (BRANCH_NAME == 'devel') {
                            def timestamp = new Date().format('yyyyMMddHHmmss')
                            sh "yap build rocky build -r ${timestamp} -sd"
                        } else {
                            sh 'yap build rocky build -sd'
                        }
                    }
                    stash includes: 'artifacts/*.rpm', name: 'artifacts-rocky'
                }
            }
            post {
                always {
                    archiveArtifacts artifacts: "artifacts/*.rpm", fingerprint: true
                }
            }
        }
        stage('Upload To Devel') {
            when {
                branch 'devel'
            }
            steps {
                unstash 'artifacts-ubuntu'
                unstash 'artifacts-rocky'

                script {
                    def server = Artifactory.server 'zextras-artifactory'
                    def buildInfo
                    def uploadSpec

                    buildInfo = Artifactory.newBuildInfo()
                    uploadSpec = """{
                        "files": [
                            {
                                "pattern": "artifacts/*.deb",
                                "target": "ubuntu-devel/pool/",
                                "props": "deb.distribution=focal;deb.distribution=jammy;deb.distribution=noble;deb.component=main;deb.architecture=amd64;vcs.revision=${env.GIT_COMMIT}"
                            },
                            {
                                "pattern": "artifacts/(service-discover-server)-(*).x86_64.rpm",
                                "target": "centos8-devel/zextras/{1}/{1}-{2}.x86_64.rpm",
                                "props": "rpm.metadata.arch=x86_64;rpm.metadata.vendor=zextras;vcs.revision=${env.GIT_COMMIT}"
                            },
                            {
                                "pattern": "artifacts/(service-discover-agent)-(*).x86_64.rpm",
                                "target": "centos8-devel/zextras/{1}/{1}-{2}.x86_64.rpm",
                                "props": "rpm.metadata.arch=x86_64;rpm.metadata.vendor=zextras;vcs.revision=${env.GIT_COMMIT}"
                            },
                            {
                                "pattern": "artifacts/(service-discover-daemon)-(*).x86_64.rpm",
                                "target": "centos8-devel/zextras/{1}/{1}-{2}.x86_64.rpm",
                                "props": "rpm.metadata.arch=x86_64;rpm.metadata.vendor=zextras;vcs.revision=${env.GIT_COMMIT}"
                            },
                            {
                                "pattern": "artifacts/(service-discover-server)-(*).x86_64.rpm",
                                "target": "centos8-devel/zextras/{1}/{1}-{2}.x86_64.rpm",
                                "props": "rpm.metadata.arch=x86_64;rpm.metadata.vendor=zextras;vcs.revision=${env.GIT_COMMIT}"
                            },
                            {
                                "pattern": "artifacts/(service-discover-agent)-(*).x86_64.rpm",
                                "target": "centos8-devel/zextras/{1}/{1}-{2}.x86_64.rpm",
                                "props": "rpm.metadata.arch=x86_64;rpm.metadata.vendor=zextras;vcs.revision=${env.GIT_COMMIT}"
                            },
                            {
                                "pattern": "artifacts/(service-discover-daemon)-(*).x86_64.rpm",
                                "target": "centos8-devel/zextras/{1}/{1}-{2}.x86_64.rpm",
                                "props": "rpm.metadata.arch=x86_64;rpm.metadata.vendor=zextras;vcs.revision=${env.GIT_COMMIT}"
                            },
                            {
                                "pattern": "artifacts/(service-discover-server)-(*).x86_64.rpm",
                                "target": "rhel9-devel/zextras/{1}/{1}-{2}.x86_64.rpm",
                                "props": "rpm.metadata.arch=x86_64;rpm.metadata.vendor=zextras;vcs.revision=${env.GIT_COMMIT}"
                            },
                            {
                                "pattern": "artifacts/(service-discover-agent)-(*).x86_64.rpm",
                                "target": "rhel9-devel/zextras/{1}/{1}-{2}.x86_64.rpm",
                                "props": "rpm.metadata.arch=x86_64;rpm.metadata.vendor=zextras;vcs.revision=${env.GIT_COMMIT}"
                            },
                            {
                                "pattern": "artifacts/(service-discover-daemon)-(*).x86_64.rpm",
                                "target": "rhel9-devel/zextras/{1}/{1}-{2}.x86_64.rpm",
                                "props": "rpm.metadata.arch=x86_64;rpm.metadata.vendor=zextras;vcs.revision=${env.GIT_COMMIT}"
                            },
                            {
                                "pattern": "artifacts/(service-discover-server)-(*).x86_64.rpm",
                                "target": "rhel9-devel/zextras/{1}/{1}-{2}.x86_64.rpm",
                                "props": "rpm.metadata.arch=x86_64;rpm.metadata.vendor=zextras;vcs.revision=${env.GIT_COMMIT}"
                            },
                            {
                                "pattern": "artifacts/(service-discover-agent)-(*).x86_64.rpm",
                                "target": "rhel9-devel/zextras/{1}/{1}-{2}.x86_64.rpm",
                                "props": "rpm.metadata.arch=x86_64;rpm.metadata.vendor=zextras;vcs.revision=${env.GIT_COMMIT}"
                            },
                            {
                                "pattern": "artifacts/(service-discover-daemon)-(*).x86_64.rpm",
                                "target": "rhel9-devel/zextras/{1}/{1}-{2}.x86_64.rpm",
                                "props": "rpm.metadata.arch=x86_64;rpm.metadata.vendor=zextras;vcs.revision=${env.GIT_COMMIT}"
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
                unstash 'artifacts-ubuntu'
                unstash 'artifacts-rocky'

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
                                "pattern": "artifacts/*.deb",
                                "target": "ubuntu-rc/pool/",
                                "props": "deb.distribution=focal;deb.distribution=jammy;deb.distribution=noble;deb.component=main;deb.architecture=amd64;vcs.revision=${env.GIT_COMMIT}"
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
                                "pattern": "artifacts/(service-discover-server)-(*).x86_64.rpm",
                                "target": "centos8-rc/zextras/{1}/{1}-{2}.x86_64.rpm",
                                "props": "rpm.metadata.arch=x86_64;rpm.metadata.vendor=zextras;vcs.revision=${env.GIT_COMMIT}"
                            },
                            {
                                "pattern": "artifacts/(service-discover-agent)-(*).x86_64.rpm",
                                "target": "centos8-rc/zextras/{1}/{1}-{2}.x86_64.rpm",
                                "props": "rpm.metadata.arch=x86_64;rpm.metadata.vendor=zextras;vcs.revision=${env.GIT_COMMIT}"
                            },
                            {
                                "pattern": "artifacts/(service-discover-daemon)-(*).x86_64.rpm",
                                "target": "centos8-rc/zextras/{1}/{1}-{2}.x86_64.rpm",
                                "props": "rpm.metadata.arch=x86_64;rpm.metadata.vendor=zextras;vcs.revision=${env.GIT_COMMIT}"
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

                    //rhel9
                    buildInfo = Artifactory.newBuildInfo()
                    buildInfo.name += "-rhel9"
                    uploadSpec= """{
                        "files": [
                            {
                                "pattern": "artifacts/(service-discover-server)-(*).x86_64.rpm",
                                "target": "rhel9-rc/zextras/{1}/{1}-{2}.x86_64.rpm",
                                "props": "rpm.metadata.arch=x86_64;rpm.metadata.vendor=zextras;vcs.revision=${env.GIT_COMMIT}"
                            },
                            {
                                "pattern": "artifacts/(service-discover-agent)-(*).x86_64.rpm",
                                "target": "rhel9-rc/zextras/{1}/{1}-{2}.x86_64.rpm",
                                "props": "rpm.metadata.arch=x86_64;rpm.metadata.vendor=zextras;vcs.revision=${env.GIT_COMMIT}"
                            },
                            {
                                "pattern": "artifacts/(service-discover-daemon)-(*).x86_64.rpm",
                                "target": "rhel9-rc/zextras/{1}/{1}-{2}.x86_64.rpm",
                                "props": "rpm.metadata.arch=x86_64;rpm.metadata.vendor=zextras;vcs.revision=${env.GIT_COMMIT}"
                            }
                        ]
                    }"""
                    server.upload spec: uploadSpec, buildInfo: buildInfo, failNoOp: false
                    config = [
                            'buildName'          : buildInfo.name,
                            'buildNumber'        : buildInfo.number,
                            'sourceRepo'         : 'rhel9-rc',
                            'targetRepo'         : 'rhel9-release',
                            'comment'            : 'Do not change anything! Just press the button',
                            'status'             : 'Released',
                            'includeDependencies': false,
                            'copy'               : true,
                            'failFast'           : true
                    ]
                    Artifactory.addInteractivePromotion server: server, promotionConfig: config, displayName: "RHEL9 Promotion to Release"
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
