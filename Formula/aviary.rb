class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.45"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.45/aviary_v0.7.45_darwin_arm64.tar.gz"
      sha256 "eedc73b4cc4dd9f9fa4a594763e5e417f8ea6accf1e838aa18b353595f5debbb"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.45/aviary_v0.7.45_darwin_amd64.tar.gz"
      sha256 "2639d2e91f1498a78e47f4321293a874c165763ff5c04b79bdf530edd0959555"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.45/aviary_v0.7.45_linux_arm64.tar.gz"
      sha256 "f0191116a7cff93d678ab49b571bc8326a40c4375e7aa05b1dbea5d8bea6d95e"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.45/aviary_v0.7.45_linux_amd64.tar.gz"
      sha256 "4e13d250e9db95eba5ae3e9855979adf82f86f2ab5d932b8977cdb1b6bcc1f46"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
