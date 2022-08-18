.PHONY: autobanh-test
autobanh-test:
	docker run -it --rm  -v ${PWD}/config:/config     -v ${PWD}/reports:/reports     crossbario/autobahn-testsuite     wstest -m fuzzingclient -s /config/config.json
