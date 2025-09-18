library(
    identifier: 'jenkins-packages-build-library@1.0.4',
    retriever: modernSCM([
        $class: 'GitSCMSource',
        remote: 'git@github.com:zextras/jenkins-packages-build-library.git',
        credentialsId: 'jenkins-integration-with-github-account'
    ])
)

pipeline {
    agent {
        node {
            label 'golang-v1'
        }
    }

    environment {
        GOPRIVATE = 'gitlab.com/zextras,bitbucket.org/zextras,github.com/zextras'
    }

    options {
        buildDiscarder(logRotator(numToKeepStr: '25'))
        skipDefaultCheckout()
        timeout(time: 2, unit: 'HOURS')
    }

    parameters {
        booleanParam defaultValue: false,
            description: 'Upload packages in playground repositories.',
            name: 'PLAYGROUND'
        booleanParam defaultValue: false,
            description: 'Set to true to skip the test stage',
            name: 'SKIP_TEST'
    }

    tools {
        jfrog 'jfrog-cli'
    }

    stages {
        stage('Stash') {
            steps {
                checkout scm
                script {
                    gitMetadata()
                }
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
                        Map modules = [:]
                        Map builds = [:]
                        modules['encrypter'] = 'pkg/encrypter'
                        modules['exec'] = 'pkg/exec'
                        modules['formatter'] = 'pkg/formatter'
                        modules['parser'] = 'pkg/parser'
                        modules['carbonio'] = 'pkg/carbonio'
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
                            junit allowEmptyResults: false, checksName: 'Test for command', testResults: 'tests.xml'
                        }
                        dir('cmd/agent') {
                            sh 'go run gotest.tools/gotestsum@latest --format testname --junitfile tests.xml'
                            junit allowEmptyResults: false, checksName: 'Test for agent', testResults: 'tests.xml'
                        }
                        dir('cmd/server') {
                            sh 'go run gotest.tools/gotestsum@latest --format testname --junitfile tests.xml'
                            junit allowEmptyResults: false, checksName: 'Test for server', testResults: 'tests.xml'
                        }
                    }
                }
            }
        }

        stage('SonarQube analysis') {
            steps {
                container('golangci-lint') {
                    script {
                        scannerHome = tool 'SonarScanner'
                    }
                    sh 'golangci-lint run ./... --issues-exit-code 0 --output.checkstyle.path linter.out'
                }
                withSonarQubeEnv(credentialsId: 'sonarqube-user-token',
                    installationName: 'SonarQube instance') {
                    sh "${scannerHome}/bin/sonar-scanner"
                }
            }
        }

        stage('Build Packages') {
            steps {
                echo 'Building deb/rpm packages'
                buildStage([
                    buildDirs: ['build'],
                    buildFlags: ' -sd ',
                    prepare: true,
                    prepareFlags: [' -g ']
                ])
            }
        }

        stage('Upload artifacts')
        {
            steps {
                uploadStage(
                    packages: yapHelper.getPackageNames('build/yap.json')
                )
            }
        }
    }

    post {
        always {
            emailext attachLog: true,
                body: '$DEFAULT_CONTENT',
                recipientProviders: [requestor()],
                subject: '$DEFAULT_SUBJECT',
                to: env.GIT_COMMIT_EMAIL
            junit allowEmptyResults: true, testResults: 'test-out/**/*.xml'
        }
    }
}
