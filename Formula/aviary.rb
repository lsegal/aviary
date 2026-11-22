class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.1"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.1/aviary_v0.7.1_darwin_arm64.tar.gz"
      sha256 "62549b59df4215020e2828ef15486cdc09af61de3ea45f19e85780ed10d51526"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.1/aviary_v0.7.1_darwin_amd64.tar.gz"
      sha256 "9d2037fdff6bce972a4c78919a295cf6d8654ed1dbe502bf5d1c931ce45e68d8"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.1/aviary_v0.7.1_linux_arm64.tar.gz"
      sha256 "61b7faf8a221ca77df5b3ef640bd760aa9d93c131e79b9178bd311835bafb27a"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.1/aviary_v0.7.1_linux_amd64.tar.gz"
      sha256 "6a50641f1c592464f44357e27a666ebd4b5ebdf7b5a28b3ee1e51e0910f1a238"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
