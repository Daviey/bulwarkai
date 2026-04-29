{
  pkgs ? import <nixpkgs> { },
}:

pkgs.mkShell {
  buildInputs = with pkgs; [
    go_1_24
    docker
    google-cloud-sdk
    terraform
    curl
    jq
  ];

  shellHook = ''
    if [ ! -f .env ]; then
      echo "WARNING: No .env file found. Copy .env.example to .env and configure it."
      echo "  cp .env.example .env"
    fi

    export PATH="$PWD:$PATH"

    echo ""
    echo "Bulwarkai dev environment"
    echo "  go        $(go version | awk '{print $3}')"
    echo "  terraform $(terraform version -json 2>/dev/null | jq -r '.terraform_version' 2>/dev/null || echo 'N/A')"
    echo ""
    echo "Commands:"
    echo "  make dev                    # run locally with .env"
    echo "  make test                   # run tests"
    echo "  make build                  # docker build"
    echo "  make deploy                 # build + push + deploy to cloud run"
    echo "  make run-local              # run in docker with .env"
    echo ""
  '';
}
