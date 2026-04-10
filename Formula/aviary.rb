class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.4.7"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.4.7/aviary_v0.4.7_darwin_arm64.tar.gz"
      sha256 "21103bca6feae088f76430bc38730c963009d3448479ea36d102a8d1713fa78d"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.4.7/aviary_v0.4.7_darwin_amd64.tar.gz"
      sha256 "a350cd868212adf00ccb22b12343fcd1f3630552030a13b7f995da43fe56d658"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.4.7/aviary_v0.4.7_linux_arm64.tar.gz"
      sha256 "2e6e8d574c4ad5c2bacf9fa259efc9c1a4e27bcbec5b07aa797fb437e58bbd6b"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.4.7/aviary_v0.4.7_linux_amd64.tar.gz"
      sha256 "fb4a50f77a7d6565e56c56949f9874a75475f0579de918e75b937207797a7a69"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
