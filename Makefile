autobanh-test:
	docker run -it --rm -v ${PWD}/config:/config -v ${PWD}/reports:/reports --add-host host.docker.internal:172.17.0.1 crossbario/autobahn-testsuite wstest -m fuzzingclient -s /config/config.json
.PHONY: autobanh-test