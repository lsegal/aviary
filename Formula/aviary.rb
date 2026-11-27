class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.83"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.83/aviary_v0.7.83_darwin_arm64.tar.gz"
      sha256 "38b15f9ced9e46108ac7faa63a0ad310f5cf37df624786b9164f3a973656faaa"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.83/aviary_v0.7.83_darwin_amd64.tar.gz"
      sha256 "bc61d8c2f623faa1953f1ec490df4a6d639ed67d454389775796f99cec467fba"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.83/aviary_v0.7.83_linux_arm64.tar.gz"
      sha256 "e29d65bc80a2411cf061de3ab9bcffe4860bab840bf186dd86f23ff1c506b269"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.83/aviary_v0.7.83_linux_amd64.tar.gz"
      sha256 "c9abf79e05bcf60ad9b2a0c9abdf30ff66d092b9a82248d5df6e7934f1719494"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
