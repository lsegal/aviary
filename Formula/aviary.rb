# frozen_string_literal: true

# Installs the Aviary CLI and server from its GitHub release binaries.
class Aviary < Formula
  desc 'Aviary: the AI Agent Nest'
  homepage 'https://aviary.bot'
  license 'MIT'
  version '1.0.6'

  on_macos do
    on_arm do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.6/aviary_v1.0.6_darwin_arm64.tar.gz'
      sha256 'cc356f558e40d9a2b1f50b712a8176b3621b3a9d4703c272df7296638f613a58'
    end

    on_intel do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.6/aviary_v1.0.6_darwin_amd64.tar.gz'
      sha256 '829faaf061b600b936df2418a3270e0ac3b0e5eedd1af32239833dd27f3166bf'
    end
  end

  on_linux do
    on_arm do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.6/aviary_v1.0.6_linux_arm64.tar.gz'
      sha256 '1277e999b3417be3122f14788d037f3d7b5229b9d6693405dbba3e8ee550e7b8'
    end

    on_intel do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.6/aviary_v1.0.6_linux_amd64.tar.gz'
      sha256 'e7282a2809503034852d2f12f09bd7d868fc61588bd576b5396234cd9b97e13b'
    end
  end

  def install
    bin.install 'aviary'
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
