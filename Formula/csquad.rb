class Csquad < Formula
  desc "Coordinate Claude Code and Codex teams in tmux"
  homepage "https://github.com/ShunL12324/c-squad"
  version "0.2.0"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.2.0/csquad_0.2.0_darwin_arm64.tar.gz"
      sha256 "7ee8bdfab74b2019eab1749b8268dd145e0022b3ddb79ca845fe12db56169492"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.2.0/csquad_0.2.0_darwin_amd64.tar.gz"
      sha256 "3c9ac20dc7d724b8735b7693f2ff8b9dc9b35910927a269f8a54f40ef74c3e3c"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.2.0/csquad_0.2.0_linux_arm64.tar.gz"
      sha256 "6f82e8b68347cf5e15af9f282c0c768abb14f76daca01a31df8835740f84a3cc"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.2.0/csquad_0.2.0_linux_amd64.tar.gz"
      sha256 "c86ee86c416a484f1227134aa086c3671bff44f17a859351a9c9ec7f87233e6a"
    end
  end

  depends_on "tmux"
  depends_on "git"

  def install
    bin.install "csquad"
    bash_completion.install "completions/csquad.bash" => "csquad"
    zsh_completion.install "completions/_csquad"
    fish_completion.install "completions/csquad.fish"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/csquad version")
    assert_match "Usage:", shell_output("#{bin}/csquad --help")
    ENV["CSQUAD_CONFIG"] = (testpath/"config.toml").to_s
    shell_output("#{bin}/csquad config")
    assert_path_exists testpath/"config.toml"
  end
end
