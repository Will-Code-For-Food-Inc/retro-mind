BUILD := build
SITE  := site

.PHONY: all clean docs docs-serve

all:
	go build -o $(BUILD)/retro-mind .

docs:
	cd $(SITE) && hugo --minify

docs-serve:
	cd $(SITE) && hugo server --bind 0.0.0.0

clean:
	rm -rf $(BUILD) $(SITE)/public
