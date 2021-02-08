pipeline {
    agent {
        node {
            label 'golang-agent-v2'
        }
    }
    options {
        buildDiscarder(logRotator(numToKeepStr: '25'))
        timeout(time: 2, unit: 'HOURS')
    }
    environment {
        GOPRIVATE="gitlab.com/zextras,bitbucket.org/zextras,github.com/zextras"
    }
    stages {
        stage('Tests') {
            steps {
                sh 'git config --global url."git@bitbucket.org:zextras".insteadOf "https://bitbucket.org/zextras"'
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
