CURRENT_CONTEXT = $(shell kubectl config current-context)

gen:
	operator-sdk generate k8s

update-dep:
	find ./vendor -type d -name testdata -exec rm -rf {} +
	bazel run //:gazelle

run:
	echo $(CURRENT_CONTEXT)
	OPERATOR_NAME=elasticsearch-operator operator-sdk up local --namespace=default

.PHONY: gen update-dep