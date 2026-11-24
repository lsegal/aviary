class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.16"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.16/aviary_v0.7.16_darwin_arm64.tar.gz"
      sha256 "412b10134b627b8a4a412631f7c238d4089664d6412c80e6f63ce27023e7c422"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.16/aviary_v0.7.16_darwin_amd64.tar.gz"
      sha256 "8cbe9910af88fe636b4a25929c089efb36c74ac9ac8a691de84dff025a719b97"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.16/aviary_v0.7.16_linux_arm64.tar.gz"
      sha256 "a91170c3c1cd553844960906a0bace3a987abb6bfed8eef36590c27c27a998e9"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.16/aviary_v0.7.16_linux_amd64.tar.gz"
      sha256 "a6e0bc83a0034ea86c00186d097540fb0f3fae527988ebfeb060d9da6b57d25e"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
