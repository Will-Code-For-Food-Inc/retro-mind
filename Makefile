BUILD := build
SITE  := site

.PHONY: all backend slim tui agent agent-backend agent-tui clean docs docs-serve

all: backend tui

backend:
	go build -o $(BUILD)/rom-tagger .

slim:
	go build -tags slim -o $(BUILD)/rom-tagger-slim .

tui:
	cd cmd/tui && go build -o ../../$(BUILD)/rom-tagger-tui .

agent: agent-backend agent-tui

agent-backend:
	go build -tags agent -o $(BUILD)/rom-tagger-agent .

agent-tui:
	cd cmd/tui && go build -tags agent -o ../../$(BUILD)/rom-tagger-tui-agent .

docs:
	gomarkdoc --config .gomarkdoc.yaml ./... ./cmd/tui/...
	cd $(SITE) && hugo --minify

docs-serve:
	gomarkdoc --config .gomarkdoc.yaml ./... ./cmd/tui/...
	cd $(SITE) && hugo server --bind 0.0.0.0

clean:
	rm -rf $(BUILD) $(SITE)/public
