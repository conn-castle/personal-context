class PersonalContext < Formula
  desc "Personal structured vault for searchable knowledge, data, files, and slides"
  homepage "https://github.com/conn-castle/personal-context"
  url "https://github.com/conn-castle/personal-context/releases/download/v0.0.0/personal-context-0.0.0.tar.gz"
  sha256 "0000000000000000000000000000000000000000000000000000000000000000"
  license "PolyForm-Noncommercial-1.0.0"

  depends_on "go" => :build

  def install
    cd "cli" do
      ldflags = %W[
        -s -w
        -X main.version=v#{version}
      ].join(" ")

      system "go", "build", *std_go_args(output: bin/"pc", ldflags: ldflags), "./cmd/pc"
    end

    generate_completions_from_executable(
      bin/"pc",
      shell_parameter_format: :cobra,
      shells:                 [:bash, :zsh, :fish],
    )
  end

  test do
    assert_match "Personal Context CLI", shell_output("#{bin}/pc --help")
    assert_match "bash completion", shell_output("#{bin}/pc completion bash")
  end
end
