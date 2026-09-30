class Csquad < Formula
  desc "Queue tasks as Claude Code background sessions"
  homepage "https://github.com/ShunL12324/c-squad"
  version "0.13.1"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.13.1/csquad_0.13.1_darwin_arm64.tar.gz"
      sha256 "4e42e7edbeeb82caaf5a997b22eef53f596d0bfbfcb9516632adc3f032a7cb1b"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.13.1/csquad_0.13.1_darwin_amd64.tar.gz"
      sha256 "87aea36c8873062d48aef36e4322c7c2ca8451f36814c073e006aa91375a8270"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.13.1/csquad_0.13.1_linux_arm64.tar.gz"
      sha256 "1b6700798a1ff7eddc248b632c434c0df6469b940fb1fafa7d90278b20b0fd52"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.13.1/csquad_0.13.1_linux_amd64.tar.gz"
      sha256 "b81a3995cb541932b76db6a6d146715ef047f931e4b2fa7bafef86003f161a15"
    end
  end

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
    ENV["XDG_CONFIG_HOME"] = testpath.to_s
    (testpath/"csquad").mkpath
    (testpath/"csquad/config.toml").write "slots = 2\n"
    assert_match "slots = 2", shell_output("#{bin}/csquad config")
  end
end
