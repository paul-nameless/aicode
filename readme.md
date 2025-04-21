# AiCode

## Usage

  docker build -t aicode .
  docker run --rm -it -v $PWD:/app -e OPENAI_API_KEY=$(pass show i/openai.com-api-key| head -n 1) aicode
