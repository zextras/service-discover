library identifier: 'mailbox-packages-lib@master', retriever: modernSCM(
        [$class: 'GitSCMSource',
         remote: 'git@github.com:zextras/jenkins-packages-build-library.git',
         credentialsId: 'jenkins-integration-with-github-account'])
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
        stage ('Build Packages') {
            steps {
                script {
                    buildStage(getPackages(), 'project', '.')()
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
