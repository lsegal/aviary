# frozen_string_literal: true

# Installs the Aviary CLI and server from its GitHub release binaries.
class Aviary < Formula
  desc 'Aviary: the AI Agent Nest'
  homepage 'https://aviary.bot'
  license 'MIT'
  version '1.0.7'

  on_macos do
    on_arm do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.7/aviary_v1.0.7_darwin_arm64.tar.gz'
      sha256 '2fb155633b3af8ea06091a867cdcc9d3570e124016b76cf2c0e40f67fd81d2c3'
    end

    on_intel do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.7/aviary_v1.0.7_darwin_amd64.tar.gz'
      sha256 '2b65d99300e5bc431fe6790f5f5b00a32fbda215fc62a63c20ef9d603040d965'
    end
  end

  on_linux do
    on_arm do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.7/aviary_v1.0.7_linux_arm64.tar.gz'
      sha256 '1ab5f36263a9df27836c02480c4e628b8b3755c56a82a1cefb7a8691e702fe22'
    end

    on_intel do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.7/aviary_v1.0.7_linux_amd64.tar.gz'
      sha256 '326c0aeba4881541fdc1afa82d7f8239b3176c9a61ab58564f5632ede47813c2'
    end
  end

  def install
    bin.install 'aviary'
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
