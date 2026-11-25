class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.50"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.50/aviary_v0.7.50_darwin_arm64.tar.gz"
      sha256 "91c2726c7ee7ef8bedd577b59d94a06196ac36a9b32c0c0cfdbc97eb80c499a6"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.50/aviary_v0.7.50_darwin_amd64.tar.gz"
      sha256 "ad7376fdd56175de21ff61ee373adbccd5042a7b4030324b28bc94fb923d39ff"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.50/aviary_v0.7.50_linux_arm64.tar.gz"
      sha256 "71cdf85fd154a246b93357db6cff184420a97e150240c9685d56c752090e9bc9"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.50/aviary_v0.7.50_linux_amd64.tar.gz"
      sha256 "03cb31877ef5df6d42c4f2aed56c45c5532cb733b2d8b12d0a8109572ca7ea00"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
